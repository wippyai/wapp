package wapp

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hashicorp/go-msgpack/v2/codec"
	"github.com/klauspost/compress/zstd"
)

const (
	// maxFrameSize is the maximum size for a data frame (10MB).
	maxFrameSize = 10 * 1024 * 1024
)

// DefaultCompressionFunc provides default logic for compression decision.
// Skips compression for already-compressed formats.
var DefaultCompressionFunc = func(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	_, skip := defaultSkipCompressedExts[ext]
	return !skip
}

var defaultSkipCompressedExts = map[string]struct{}{
	".png": {}, ".jpg": {}, ".jpeg": {}, ".gif": {},
	".webp": {}, ".ico": {},
	".woff": {}, ".woff2": {}, ".ttf": {}, ".otf": {},
	".mp4": {}, ".webm": {}, ".mp3": {}, ".ogg": {},
	".wav": {}, ".avi": {}, ".mov": {},
	".gz": {}, ".zip": {}, ".br": {}, ".zst": {},
	".7z": {}, ".rar": {}, ".bz2": {}, ".xz": {},
}

// encodedEntry is the serialization format for entries.
type encodedEntry struct {
	ID   ID       `msgpack:"ID"`
	Kind string   `msgpack:"Kind"`
	Meta Metadata `msgpack:"Meta,omitempty"`
	Data any      `msgpack:"Data,omitempty"`
}

// ProgressCallback reports packing progress.
type ProgressCallback func(resourceID ID, current, total int)

// Writer writes WAPP files.
type Writer struct {
	handle           *codec.MsgpackHandle
	metadataLevel    zstd.EncoderLevel
	entriesLevel     zstd.EncoderLevel
	tocLevel         zstd.EncoderLevel
	compressionFunc  func(string) bool
	progressCallback ProgressCallback
}

// WriterOption configures Writer.
type WriterOption func(*Writer)

// WithProgressCallback sets a progress callback.
func WithProgressCallback(fn ProgressCallback) WriterOption {
	return func(w *Writer) {
		w.progressCallback = fn
	}
}

// WithCompressionFunc sets custom compression decision function.
func WithCompressionFunc(fn func(string) bool) WriterOption {
	return func(w *Writer) {
		w.compressionFunc = fn
	}
}

// NewWriter creates a new WAPP writer.
func NewWriter(opts ...WriterOption) *Writer {
	writer := &Writer{
		handle:          newMsgpackHandle(),
		metadataLevel:   zstd.SpeedDefault,
		entriesLevel:    zstd.SpeedDefault,
		tocLevel:        zstd.SpeedDefault,
		compressionFunc: DefaultCompressionFunc,
	}

	for _, opt := range opts {
		opt(writer)
	}

	return writer
}

// PackEntries creates a WAPP file with only metadata and entries.
func (w *Writer) PackEntries(
	metadata Metadata,
	entries []Entry,
	out io.Writer,
) error {
	encodedEntries := make([]encodedEntry, len(entries))
	for i, entry := range entries {
		encodedEntries[i] = encodedEntry(entry)
	}

	metaFrame, metaInfo, err := w.createMetadataFrame(metadata)
	if err != nil {
		return errCreateMetadataFrame(err)
	}

	entriesFrame, entriesInfo, err := w.createEntriesFrame(encodedEntries)
	if err != nil {
		return errCreateEntriesFrame(err)
	}

	toc := &TOC{
		Metadata:   metaInfo,
		Entries:    entriesInfo,
		Resources:  nil,
		DataFrames: nil,
	}

	allFrames := []rawFrame{metaFrame, entriesFrame}
	return w.writePack(out, toc, allFrames)
}

// Pack creates a WAPP file from filesystem and entries.
func (w *Writer) Pack(
	metadata Metadata,
	entries []Entry,
	fsys fs.FS,
	resourceID ID,
	resourceMeta Metadata,
	out io.Writer,
) error {
	encodedEntries := make([]encodedEntry, len(entries))
	for i, entry := range entries {
		encodedEntries[i] = encodedEntry(entry)
	}

	metaFrame, metaInfo, err := w.createMetadataFrame(metadata)
	if err != nil {
		return errCreateMetadataFrame(err)
	}

	entriesFrame, entriesInfo, err := w.createEntriesFrame(encodedEntries)
	if err != nil {
		return errCreateEntriesFrame(err)
	}

	tree, dataFrames, err := w.processFilesystem(fsys, resourceID, resourceMeta)
	if err != nil {
		return errProcessFilesystem(err)
	}

	resourceFrame, resourceInfo, err := w.createResourceFrame(tree)
	if err != nil {
		return errCreateResourceFrame("", err)
	}

	toc := &TOC{
		Metadata:  metaInfo,
		Entries:   entriesInfo,
		Resources: []ResourceFrame{resourceInfo},
	}

	allFrames := []rawFrame{metaFrame, entriesFrame, resourceFrame}
	allFrames = append(allFrames, dataFrames...)

	return w.writePack(out, toc, allFrames)
}

// PackWithResources creates a WAPP file with multiple filesystem resources.
func (w *Writer) PackWithResources(
	metadata Metadata,
	entries []Entry,
	resources []ResourceSpec,
	out io.Writer,
) error {
	encodedEntries := make([]encodedEntry, len(entries))
	for i, entry := range entries {
		encodedEntries[i] = encodedEntry(entry)
	}

	metaFrame, metaInfo, err := w.createMetadataFrame(metadata)
	if err != nil {
		return errCreateMetadataFrame(err)
	}

	entriesFrame, entriesInfo, err := w.createEntriesFrame(encodedEntries)
	if err != nil {
		return errCreateEntriesFrame(err)
	}

	resourceInfos := make([]ResourceFrame, 0, len(resources))
	resourceFrames := make([]rawFrame, 0, len(resources))
	allDataFrames := make([]rawFrame, 0)

	if len(resources) > int(^uint32(0)-2) {
		return errInvalidResourceCount(len(resources))
	}
	dataFrameStartIndex := 2 + uint32(len(resources))

	for _, spec := range resources {
		fsys := spec.FS
		if fsys == nil {
			return errInvalidResourceFS(spec.ID.String())
		}

		if len(allDataFrames) > int(^uint32(0)) {
			return errInvalidResourceCount(len(allDataFrames))
		}
		tree, dataFrames, err := w.processFilesystemWithOffset(fsys, spec.ID, spec.Meta, dataFrameStartIndex+uint32(len(allDataFrames)))
		if err != nil {
			return errProcessResourceFilesystem(spec.ID.String(), err)
		}

		resourceFrame, resourceInfo, err := w.createResourceFrame(tree)
		if err != nil {
			return errCreateResourceFrame(spec.ID.String(), err)
		}

		resourceInfos = append(resourceInfos, resourceInfo)
		resourceFrames = append(resourceFrames, resourceFrame)
		allDataFrames = append(allDataFrames, dataFrames...)
	}

	toc := &TOC{
		Metadata:  metaInfo,
		Entries:   entriesInfo,
		Resources: resourceInfos,
	}

	allFrames := []rawFrame{metaFrame, entriesFrame}
	allFrames = append(allFrames, resourceFrames...)
	allFrames = append(allFrames, allDataFrames...)

	return w.writePack(out, toc, allFrames)
}

type rawFrame struct {
	data             []byte
	compressed       bool
	uncompressedSize uint64
}

func (w *Writer) processFilesystem(
	fsys fs.FS,
	id ID,
	meta Metadata,
) (*TreeResource, []rawFrame, error) {
	return w.processFilesystemWithOffset(fsys, id, meta, 3)
}

func (w *Writer) processFilesystemWithOffset(
	fsys fs.FS,
	id ID,
	meta Metadata,
	startFrameIndex uint32,
) (*TreeResource, []rawFrame, error) {
	tree := &TreeResource{
		ID:    id,
		Meta:  meta,
		Files: make(map[string]*FileEntry),
		Dirs:  make(map[string][]string),
	}

	totalFiles := 0
	if w.progressCallback != nil {
		_ = fs.WalkDir(fsys, ".", func(_ string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			totalFiles++
			return nil
		})
	}

	var dataFrames []rawFrame
	currentFrame := &bytes.Buffer{}
	frameIndex := startFrameIndex
	filesProcessed := 0

	err := fs.WalkDir(fsys, ".", func(filePath string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		filePath = path.Clean(filePath)
		if filePath == "." {
			filePath = ""
		}

		if d.IsDir() {
			if filePath != "" {
				if _, ok := tree.Dirs[filePath]; !ok {
					tree.Dirs[filePath] = []string(nil)
				}
				// Add directory to parent directory's children list.
				parentDir := path.Dir(filePath)
				if parentDir == "." {
					parentDir = ""
				}
				if children, ok := tree.Dirs[parentDir]; ok {
					tree.Dirs[parentDir] = append(children, path.Base(filePath))
				} else {
					tree.Dirs[parentDir] = []string{path.Base(filePath)}
				}
			}
			return nil
		}

		fileInfo, err := d.Info()
		if err != nil {
			return errStatFile(filePath, err)
		}

		file, err := fsys.Open(filePath)
		if err != nil {
			return errOpenFile(filePath, err)
		}

		fileData, err := io.ReadAll(file)
		_ = file.Close()
		if err != nil {
			return errReadFile(filePath, err)
		}

		hashBytes := sha256.Sum256(fileData)
		hash := hex.EncodeToString(hashBytes[:])

		shouldCompress := w.shouldCompressFile(filePath)

		var finalData []byte
		var compressed bool

		if shouldCompress && len(fileData) > 0 {
			var buf bytes.Buffer
			if err := compressZstd(fileData, &buf); err != nil {
				return errCompressFile(filePath, err)
			}
			finalData = buf.Bytes()
			compressed = true
		} else {
			finalData = fileData
			compressed = false
		}

		var chunks []ChunkInfo
		var location FileLocation

		if uint64(len(finalData)) > ChunkSize {
			chunks = make([]ChunkInfo, 0)
			dataOffset := uint64(0)

			for dataOffset < uint64(len(finalData)) {
				chunkSize := ChunkSize
				if dataOffset+chunkSize > uint64(len(finalData)) {
					chunkSize = uint64(len(finalData)) - dataOffset
				}

				chunkData := finalData[dataOffset : dataOffset+chunkSize]

				if currentFrame.Len()+len(chunkData) > maxFrameSize && currentFrame.Len() > 0 {
					dataFrames = append(dataFrames, rawFrame{
						data:             currentFrame.Bytes(),
						compressed:       false,
						uncompressedSize: uint64(currentFrame.Len()),
					})
					currentFrame = &bytes.Buffer{}
					frameIndex++
				}

				chunks = append(chunks, ChunkInfo{
					Offset:      dataOffset,
					Size:        uint32(chunkSize),
					FrameIndex:  frameIndex,
					FrameOffset: uint64(currentFrame.Len()),
				})

				currentFrame.Write(chunkData)
				dataOffset += chunkSize
			}

			location = FileLocation{
				FrameIndex: chunks[0].FrameIndex,
				Offset:     chunks[0].FrameOffset,
				Chunks:     chunks,
			}
		} else {
			if currentFrame.Len()+len(finalData) > maxFrameSize && currentFrame.Len() > 0 {
				dataFrames = append(dataFrames, rawFrame{
					data:             currentFrame.Bytes(),
					compressed:       false,
					uncompressedSize: uint64(currentFrame.Len()),
				})
				currentFrame = &bytes.Buffer{}
				frameIndex++
			}

			location = FileLocation{
				FrameIndex: frameIndex,
				Offset:     uint64(currentFrame.Len()),
				Chunks:     nil,
			}

			currentFrame.Write(finalData)
		}

		entry := FileEntry{
			Size:       uint64(len(fileData)),
			Mode:       uint32(fileInfo.Mode()),
			ModTime:    fileInfo.ModTime().Unix(),
			Hash:       hash,
			Compressed: compressed,
			Meta:       nil,
			Location:   location,
		}

		if compressed {
			entry.CompressedSize = uint64(len(finalData))
		}

		tree.Files[filePath] = &entry

		dir := path.Dir(filePath)
		if dir == "." {
			dir = ""
		}
		if children, ok := tree.Dirs[dir]; ok {
			tree.Dirs[dir] = append(children, path.Base(filePath))
		} else {
			tree.Dirs[dir] = []string{path.Base(filePath)}
		}

		filesProcessed++
		if w.progressCallback != nil && totalFiles > 0 {
			w.progressCallback(id, filesProcessed, totalFiles)
		}

		return nil
	})

	if err != nil {
		return nil, nil, err
	}

	if currentFrame.Len() > 0 {
		dataFrames = append(dataFrames, rawFrame{
			data:             currentFrame.Bytes(),
			compressed:       false,
			uncompressedSize: uint64(currentFrame.Len()),
		})
	}

	for dir := range tree.Dirs {
		sort.Strings(tree.Dirs[dir])
	}

	return tree, dataFrames, nil
}

func (w *Writer) shouldCompressFile(filename string) bool {
	return w.compressionFunc(filename)
}

func (w *Writer) createMetadataFrame(metadata Metadata) (rawFrame, FrameInfo, error) {
	return w.createCompressedFrame(metadata, w.metadataLevel)
}

func (w *Writer) createEntriesFrame(entries []encodedEntry) (rawFrame, FrameInfo, error) {
	return w.createCompressedFrame(entries, w.entriesLevel)
}

func (w *Writer) createResourceFrame(tree *TreeResource) (rawFrame, ResourceFrame, error) {
	frame, frameInfo, err := w.createCompressedFrame(tree, zstd.SpeedDefault)
	if err != nil {
		return rawFrame{}, ResourceFrame{}, err
	}

	var totalSize uint64
	for _, f := range tree.Files {
		totalSize += f.Size
	}

		return frame, ResourceFrame{
			ID:        tree.ID,
			Type:      ResourceTypeTree,
			Meta:      tree.Meta,
			Frame:     frameInfo,
			FileCount: uint32(len(tree.Files)),
			TotalSize: totalSize,
		}, nil
}

func (w *Writer) createCompressedFrame(data interface{}, level zstd.EncoderLevel) (rawFrame, FrameInfo, error) {
	var buf bytes.Buffer
	encoder := codec.NewEncoder(&buf, w.handle)
	if err := encoder.Encode(data); err != nil {
		return rawFrame{}, FrameInfo{}, errMsgpackEncode(err)
	}

	uncompData := buf.Bytes()
	uncompSize := uint64(len(uncompData))

	var compBuf bytes.Buffer
	if err := compressZstdWithLevel(uncompData, &compBuf, level); err != nil {
		return rawFrame{}, FrameInfo{}, errZstdCompress(err)
	}

	compData := compBuf.Bytes()
	hashBytes := sha256.Sum256(compData)
	hash := hex.EncodeToString(hashBytes[:])

	return rawFrame{
			data:             compData,
			compressed:       true,
			uncompressedSize: uncompSize,
		}, FrameInfo{
			Size:             uint64(len(compData)),
			UncompressedSize: uncompSize,
			Hash:             hash,
		}, nil
}

func (w *Writer) writePack(out io.Writer, toc *TOC, frames []rawFrame) error {
	dataOffset := uint64(HeaderSize)
	currentOffset := dataOffset

	if len(frames) >= 1 {
		toc.Metadata.Offset = currentOffset
		currentOffset += uint64(len(frames[0].data))
	}

	if len(frames) >= 2 {
		toc.Entries.Offset = currentOffset
		currentOffset += uint64(len(frames[1].data))
	}

	numResources := len(toc.Resources)
	frameIdx := 2

	expectedMinFrames := 2 + numResources
	if len(frames) < expectedMinFrames {
		return errInsufficientFrames(len(frames), expectedMinFrames)
	}

	for i := range toc.Resources {
		toc.Resources[i].Frame.Offset = currentOffset
		toc.Resources[i].Frame.Size = uint64(len(frames[frameIdx].data))
		toc.Resources[i].Frame.UncompressedSize = frames[frameIdx].uncompressedSize
		currentOffset += uint64(len(frames[frameIdx].data))
		frameIdx++
	}

	dataFrameStart := 2 + numResources
	numDataFrames := len(frames) - dataFrameStart
	toc.DataFrames = make([]FrameInfo, numDataFrames)

	for i := 0; i < numDataFrames; i++ {
		hashBytes := sha256.Sum256(frames[frameIdx].data)
		hash := hex.EncodeToString(hashBytes[:])
		toc.DataFrames[i] = FrameInfo{
			Offset:           currentOffset,
			Size:             uint64(len(frames[frameIdx].data)),
			UncompressedSize: frames[frameIdx].uncompressedSize,
			Hash:             hash,
		}
		currentOffset += uint64(len(frames[frameIdx].data))
		frameIdx++
	}

	dataSize := uint64(0)
	for _, frame := range frames {
		dataSize += uint64(len(frame.data))
	}

	header := &Header{}
	header.DataOffset = dataOffset
	header.DataSize = dataSize

	hasher := sha256.New()
	for _, frame := range frames {
		hasher.Write(frame.data)
	}
	dataHashBytes := hasher.Sum(nil)
	copy(header.DataHash[:], dataHashBytes)

	if err := WriteHeader(out, header); err != nil {
		return errWriteHeader(err)
	}

	for i, frame := range frames {
		if _, err := out.Write(frame.data); err != nil {
			return errWriteFrame(i, err)
		}
	}

	tocOffset := dataOffset + dataSize

	tocData, tocInfo, err := w.createCompressedFrame(toc, w.tocLevel)
	if err != nil {
		return errCreateTOCFrame(err)
	}

	if _, err := out.Write(tocData.data); err != nil {
		return errWriteTOC(err)
	}

	footer := &Footer{
		TOCOffset: tocOffset,
		TOCSize:   tocInfo.Size,
	}

	if err := WriteFooter(out, footer); err != nil {
		return errWriteFooter(err)
	}

	return nil
}
