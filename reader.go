package wapp

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"strings"
	"sync"

	"github.com/hashicorp/go-msgpack/v2/codec"
)

const (
	hashChunkSize = 32 * 1024 // 32KB chunks for streaming hash
	maxAllocSize  = int(^uint(0) >> 1)
	// maxReadAtOffset caps offsets to int64-compatible ranges.
	maxReadAtOffset = int64(^uint64(0) >> 1)
	// defaultDecompressionCacheBytes bounds the per-reader decompression cache.
	defaultDecompressionCacheBytes = 64 << 20
	// maxDataSize limits the data section to avoid excessive allocations.
	maxDataSize = 1 << 30
)

// Reader reads WAPP files with lazy loading.
type Reader struct {
	reader io.ReaderAt
	header *Header
	footer *Footer
	toc    *TOC
	handle *codec.MsgpackHandle
	cache  *decompressionCache

	// Resource index for O(1) lookup.
	resourceIndex map[ID]int

	// Lazy loaded data.
	metadata        Metadata
	metadataOnce    sync.Once
	metadataErr     error
	entries         []Entry
	entriesOnce     sync.Once
	entriesErr      error
	resources       []any
	resourcesLoaded []bool
	resourcesMutex  sync.RWMutex
}

// ReaderOption configures Reader behavior.
type ReaderOption func(*Reader)

// WithDecompressionCacheLimit sets the maximum cached decompressed bytes.
// Use 0 to disable the cache.
func WithDecompressionCacheLimit(maxBytes int64) ReaderOption {
	return func(r *Reader) {
		r.cache = newDecompressionCache(maxBytes)
	}
}

// NewReader creates a WAPP reader using footer-first reading.
// It uses the default decompression cache limit.
func NewReader(r io.ReaderAt) (*Reader, error) {
	return NewReaderWithOptions(r)
}

// NewReaderWithOptions creates a WAPP reader with options.
func NewReaderWithOptions(r io.ReaderAt, opts ...ReaderOption) (*Reader, error) {
	reader := &Reader{
		reader: r,
		handle: newMsgpackHandle(),
		cache:  newDecompressionCache(defaultDecompressionCacheBytes),
	}

	for _, opt := range opts {
		opt(reader)
	}

	// Read header.
	headerBuf := make([]byte, HeaderSize)
	if _, err := r.ReadAt(headerBuf, 0); err != nil {
		return nil, errReadHeader(err)
	}

	header, err := ReadHeader(bytes.NewReader(headerBuf))
	if err != nil {
		return nil, err
	}
	reader.header = header

	// Validate data size to prevent memory exhaustion.
	if header.DataSize > maxDataSize {
		return nil, errDataSizeExceedsMax(header.DataSize, maxDataSize)
	}

	// Validate data hash using streaming to avoid large allocation.
	if err := reader.validateDataHash(header); err != nil {
		return nil, err
	}

	// Read footer.
	var rs io.ReadSeeker
	if seeker, ok := r.(io.ReadSeeker); ok {
		rs = seeker
	} else {
		rs = io.NewSectionReader(r, 0, 1<<63-1)
	}
	footer, err := ReadFooter(rs)
	if err != nil {
		return nil, errReadFooter(err)
	}
	reader.footer = footer

	fileSize := int64(-1)
	if seeker, ok := r.(io.Seeker); ok {
		cur, err := seeker.Seek(0, io.SeekCurrent)
		if err == nil {
			end, err := seeker.Seek(0, io.SeekEnd)
			if err == nil {
				fileSize = end
			}
			_, _ = seeker.Seek(cur, io.SeekStart)
		}
	}

	if err := reader.validateTOCPlacement(fileSize); err != nil {
		return nil, err
	}

	// Read and decompress TOC.
	tocBuf, err := makeFrameBuffer(footer.TOCSize)
	if err != nil {
		return nil, err
	}
	if _, err := r.ReadAt(tocBuf, int64(footer.TOCOffset)); err != nil {
		return nil, errReadTOC(err)
	}

	toc, err := reader.decompressFrame(tocBuf)
	if err != nil {
		return nil, errDecompressTOC(err)
	}

	reader.toc = &TOC{}
	decoder := codec.NewDecoder(bytes.NewReader(toc), reader.handle)
	if err := decoder.Decode(reader.toc); err != nil {
		return nil, errDecodeTOC(err)
	}

	if err := reader.validateTOCFrames(); err != nil {
		return nil, err
	}

	// Build resource index for O(1) lookup.
	reader.resourceIndex = make(map[ID]int, len(reader.toc.Resources))
	reader.resources = make([]any, len(reader.toc.Resources))
	reader.resourcesLoaded = make([]bool, len(reader.toc.Resources))
	for i := range reader.toc.Resources {
		reader.resourceIndex[reader.toc.Resources[i].ID] = i
	}

	return reader, nil
}

// validateDataHash validates data integrity using streaming or direct hash.
func (r *Reader) validateDataHash(header *Header) error {
	// For small data, direct read is faster.
	if header.DataSize <= hashChunkSize {
		buf := make([]byte, header.DataSize)
		if _, err := r.reader.ReadAt(buf, int64(header.DataOffset)); err != nil {
			return errReadData(err)
		}
		hash := sha256.Sum256(buf)
		if !bytes.Equal(hash[:], header.DataHash[:]) {
			return ErrDataCorrupted
		}
		return nil
	}

	// Stream hash for large data to avoid huge allocation.
	h := sha256.New()
	buf := make([]byte, hashChunkSize)
	remaining := header.DataSize
	offset := int64(header.DataOffset)

	for remaining > 0 {
		toRead := uint64(len(buf))
		if toRead > remaining {
			toRead = remaining
		}

		n, err := r.reader.ReadAt(buf[:toRead], offset)
		if n == 0 {
			if err == io.EOF {
				return errReadData(io.ErrUnexpectedEOF)
			}
			if err != nil {
				return errReadData(err)
			}
			return errReadData(io.ErrUnexpectedEOF)
		}
		if err != nil && err != io.EOF {
			return errReadData(err)
		}

		h.Write(buf[:n])
		remaining -= uint64(n)
		offset += int64(n)
	}

	var sum [32]byte
	h.Sum(sum[:0])
	if !bytes.Equal(sum[:], header.DataHash[:]) {
		return ErrDataCorrupted
	}
	return nil
}

func checkedAddUint64(left, right uint64) (uint64, bool) {
	if left > ^uint64(0)-right {
		return 0, false
	}
	return left + right, true
}

func (r *Reader) validateTOCPlacement(fileSize int64) error {
	dataEnd, ok := checkedAddUint64(r.header.DataOffset, r.header.DataSize)
	if !ok {
		return errInvalidTOC("data size overflows offset")
	}

	if r.footer.TOCSize == 0 {
		return errInvalidTOC("toc size is zero")
	}

	if r.footer.TOCOffset < dataEnd {
		return errInvalidTOC(fmt.Sprintf("toc offset %d before data end %d", r.footer.TOCOffset, dataEnd))
	}

	if r.footer.TOCOffset > ^uint64(0)-r.footer.TOCSize {
		return errInvalidTOC("toc offset overflows toc size")
	}

	if err := validateReadAtRange(r.footer.TOCOffset, r.footer.TOCSize); err != nil {
		return errInvalidTOC(err.Error())
	}

	if fileSize >= 0 {
		if fileSize < FooterSize {
			return errInvalidTOC("file size smaller than footer")
		}
		footerStart := uint64(fileSize) - FooterSize
		tocEnd := r.footer.TOCOffset + r.footer.TOCSize
		if tocEnd > footerStart {
			return errInvalidTOC(fmt.Sprintf("toc end %d exceeds footer start %d", tocEnd, footerStart))
		}
	}

	return nil
}

func (r *Reader) validateTOCFrames() error {
	dataStart := r.header.DataOffset
	dataEnd, ok := checkedAddUint64(r.header.DataOffset, r.header.DataSize)
	if !ok {
		return errInvalidTOC("data size overflows offset")
	}

	checkFrame := func(name string, info FrameInfo) error {
		if info.Size == 0 {
			return errInvalidTOC(fmt.Sprintf("%s size is zero", name))
		}
		if info.Offset < dataStart {
			return errInvalidTOC(fmt.Sprintf("%s offset %d before data start %d", name, info.Offset, dataStart))
		}
		if info.Offset > ^uint64(0)-info.Size {
			return errInvalidTOC(fmt.Sprintf("%s offset %d overflows size %d", name, info.Offset, info.Size))
		}
		frameEnd := info.Offset + info.Size
		if frameEnd > dataEnd {
			return errInvalidTOC(fmt.Sprintf("%s end %d exceeds data end %d", name, frameEnd, dataEnd))
		}
		if err := validateReadAtRange(info.Offset, info.Size); err != nil {
			return errInvalidTOC(fmt.Sprintf("%s %s", name, err))
		}
		return nil
	}

	if err := checkFrame("metadata frame", r.toc.Metadata); err != nil {
		return err
	}
	if err := checkFrame("entries frame", r.toc.Entries); err != nil {
		return err
	}
	for i, resource := range r.toc.Resources {
		if err := checkFrame(fmt.Sprintf("resource[%d] frame", i), resource.Frame); err != nil {
			return err
		}
	}
	for i, frame := range r.toc.DataFrames {
		if err := checkFrame(fmt.Sprintf("data[%d] frame", i), frame); err != nil {
			return err
		}
	}

	return nil
}

func validateReadAtRange(offset, size uint64) error {
	if offset > uint64(maxReadAtOffset) {
		return fmt.Errorf("offset %d exceeds max %d", offset, maxReadAtOffset)
	}
	end, ok := checkedAddUint64(offset, size)
	if !ok {
		return fmt.Errorf("offset %d overflows size %d", offset, size)
	}
	if end > uint64(maxReadAtOffset) {
		return fmt.Errorf("end %d exceeds max %d", end, maxReadAtOffset)
	}
	return nil
}
func makeFrameBuffer(size uint64) ([]byte, error) {
	if size > uint64(maxAllocSize) {
		return nil, errFrameOutOfBounds(fmt.Sprintf("size %d exceeds max %d", size, maxAllocSize))
	}
	return make([]byte, size), nil
}

func (r *Reader) verifyFrameHash(info FrameInfo, data []byte) error {
	if info.Hash == "" {
		return nil
	}
	sum := sha256.Sum256(data)
	actual := hex.EncodeToString(sum[:])
	if !strings.EqualFold(actual, info.Hash) {
		return errFrameHashMismatch(fmt.Sprintf("expected %s got %s", info.Hash, actual))
	}
	return nil
}

// Header returns the WAPP header.
func (r *Reader) Header() *Header {
	return r.header
}

// GetMetadata returns pack metadata (lazy loaded).
func (r *Reader) GetMetadata() (Metadata, error) {
	r.metadataOnce.Do(func() {
		data, err := r.readFrame(r.toc.Metadata)
		if err != nil {
			r.metadataErr = errReadMetadataFrame(err)
			return
		}

		decoder := codec.NewDecoder(bytes.NewReader(data), r.handle)
		if err := decoder.Decode(&r.metadata); err != nil {
			r.metadataErr = errDecodeMetadata(err)
			return
		}
	})

	return r.metadata, r.metadataErr
}

// GetEntries returns registry entries (lazy loaded).
func (r *Reader) GetEntries() ([]Entry, error) {
	r.entriesOnce.Do(func() {
		data, err := r.readFrame(r.toc.Entries)
		if err != nil {
			r.entriesErr = errReadEntriesFrame(err)
			return
		}

		var encodedEntries []encodedEntry
		decoder := codec.NewDecoder(bytes.NewReader(data), r.handle)
		if err := decoder.Decode(&encodedEntries); err != nil {
			r.entriesErr = errDecodeEntries(err)
			return
		}

		entries := make([]Entry, len(encodedEntries))
		for i, enc := range encodedEntries {
			entries[i] = Entry(enc)
		}

		r.entries = entries
	})

	return r.entries, r.entriesErr
}

// ListResources returns resource metadata from TOC.
func (r *Reader) ListResources() []ResourceInfo {
	result := make([]ResourceInfo, len(r.toc.Resources))
	for i, r := range r.toc.Resources {
		result[i] = ResourceInfo{
			ID:        r.ID,
			Type:      r.Type,
			Meta:      r.Meta,
			Hash:      r.Frame.Hash,
			Size:      r.TotalSize,
			FileCount: r.FileCount,
		}
	}
	return result
}

// GetFS returns filesystem for a tree resource (lazy loaded).
func (r *Reader) GetFS(id ID) (fs.ReadDirFS, error) {
	res, err := r.loadResource(id)
	if err != nil {
		return nil, err
	}

	tree, ok := res.(*TreeResource)
	if !ok {
		return nil, errResourceNotTree(id.String())
	}

	return newPackFS(tree, r), nil
}

// loadResource lazy loads a resource using O(1) index lookup.
func (r *Reader) loadResource(id ID) (any, error) {
	idx, ok := r.resourceIndex[id]
	if !ok {
		return nil, errResourceNotFound(id.String())
	}

	r.resourcesMutex.RLock()
	if r.resourcesLoaded[idx] {
		res := r.resources[idx]
		r.resourcesMutex.RUnlock()
		return res, nil
	}
	r.resourcesMutex.RUnlock()

	resInfo := &r.toc.Resources[idx]
	data, err := r.readFrame(resInfo.Frame)
	if err != nil {
		return nil, errReadResourceFrame(err)
	}

	var res any
	decoder := codec.NewDecoder(bytes.NewReader(data), r.handle)

	switch resInfo.Type {
	case ResourceTypeTree:
		tree := &TreeResource{}
		if err := decoder.Decode(tree); err != nil {
			return nil, errDecodeTreeResource(err)
		}
		res = tree
	default:
		return nil, errUnknownResourceType(resInfo.Type)
	}

	r.resourcesMutex.Lock()
	r.resources[idx] = res
	r.resourcesLoaded[idx] = true
	r.resourcesMutex.Unlock()

	return res, nil
}

// readFrame reads and decompresses a frame.
func (r *Reader) readFrame(info FrameInfo) ([]byte, error) {
	if info.Size == 0 {
		return nil, errFrameOutOfBounds("frame size is zero")
	}
	buf, err := makeFrameBuffer(info.Size)
	if err != nil {
		return nil, err
	}
	if _, err := r.reader.ReadAt(buf, int64(info.Offset)); err != nil {
		return nil, errReadFrame(err)
	}
	if err := r.verifyFrameHash(info, buf); err != nil {
		return nil, err
	}

	return r.decompressFrame(buf)
}

// decompressFrame decompresses zstd compressed data.
func (r *Reader) decompressFrame(compressed []byte) ([]byte, error) {
	return decompressZstd(compressed)
}

// FrameReader interface abstracts frame data reading.
type FrameReader interface {
	ReadFrameData(frameInfo FrameInfo, offset, size uint64) ([]byte, error)
}

// ReadFrameData reads data from a specific frame at offset.
func (r *Reader) ReadFrameData(frameInfo FrameInfo, offset, size uint64) ([]byte, error) {
	return r.readFrameData(frameInfo, offset, size)
}

// readFrameData reads data from a specific frame at offset.
func (r *Reader) readFrameData(frameInfo FrameInfo, offset, size uint64) ([]byte, error) {
	if offset > frameInfo.Size {
		return nil, errFrameOutOfBounds(fmt.Sprintf("offset %d exceeds frame size %d", offset, frameInfo.Size))
	}
	if size > frameInfo.Size-offset {
		return nil, errFrameOutOfBounds(fmt.Sprintf("offset %d size %d exceeds frame size %d", offset, size, frameInfo.Size))
	}
	buf, err := makeFrameBuffer(size)
	if err != nil {
		return nil, err
	}
	if _, err := r.reader.ReadAt(buf, int64(frameInfo.Offset+offset)); err != nil {
		return nil, errReadFrameData(err)
	}
	if offset == 0 && size == frameInfo.Size {
		if err := r.verifyFrameHash(frameInfo, buf); err != nil {
			return nil, err
		}
	}
	return buf, nil
}

// getDataFrameInfo returns FrameInfo for a data frame by index.
func (r *Reader) getDataFrameInfo(frameIndex uint32) (FrameInfo, error) {
	// Data frames start at index 2 + len(resources)
	dataFrameIdx := int(frameIndex) - 2 - len(r.toc.Resources)
	if dataFrameIdx < 0 || dataFrameIdx >= len(r.toc.DataFrames) {
		return FrameInfo{}, errFrameNotFound(int(frameIndex))
	}
	return r.toc.DataFrames[dataFrameIdx], nil
}
