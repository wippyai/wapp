package wapp

import (
	"fmt"
	"io"
	"io/fs"
	"path"
	"time"
)

// packFS implements fs.ReadDirFS for tree resources.
type packFS struct {
	tree   *TreeResource
	reader *Reader

	// Cache for decompressed file content to avoid repeated decompression.
	cache       *decompressionCache
	cachePrefix string
}

// newPackFS creates a filesystem from a tree resource.
func newPackFS(tree *TreeResource, reader *Reader) fs.ReadDirFS {
	return &packFS{
		tree:        tree,
		reader:      reader,
		cache:       reader.cache,
		cachePrefix: tree.ID.String(),
	}
}

// Open implements fs.FS.
func (pfs *packFS) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}

	name = path.Clean(name)
	if name == "." {
		name = ""
	}

	if children, ok := pfs.tree.Dirs[name]; ok {
		return &packDir{
			name:     path.Base(name),
			children: children,
			pfs:      pfs,
		}, nil
	}

	if entry, ok := pfs.tree.Files[name]; ok {
		return &packFile{
			name:     path.Base(name),
			path:     name,
			entry:    entry,
			reader:   pfs.reader,
			cache:    pfs.cache,
			cacheKey: pfs.cacheKey(name),
		}, nil
	}

	return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
}

// ReadDir implements fs.ReadDirFS.
func (pfs *packFS) ReadDir(name string) ([]fs.DirEntry, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrInvalid}
	}

	name = path.Clean(name)
	if name == "." {
		name = ""
	}

	children, ok := pfs.tree.Dirs[name]
	if !ok {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrNotExist}
	}

	entries := make([]fs.DirEntry, len(children))
	for i, child := range children {
		childPath := path.Join(name, child)

		if _, isDir := pfs.tree.Dirs[childPath]; isDir {
			entries[i] = &packDirEntry{
				name:  child,
				isDir: true,
			}
		} else if fileEntry, ok := pfs.tree.Files[childPath]; ok {
			entries[i] = &packDirEntry{
				name:  child,
				isDir: false,
				size:  int64(fileEntry.Size),
				mode:  fs.FileMode(fileEntry.Mode),
				mtime: time.Unix(fileEntry.ModTime, 0),
			}
		} else {
			return nil, errInvalidTOC(fmt.Sprintf("missing child %q in %q", child, name))
		}
	}

	return entries, nil
}

func (pfs *packFS) cacheKey(path string) string {
	if pfs.cachePrefix == "" {
		return path
	}
	return pfs.cachePrefix + ":" + path
}

// packFile implements fs.File for files in the pack.
type packFile struct {
	name             string
	path             string
	entry            FileEntry
	reader           *Reader
	offset           int64
	decompressedData []byte
	cache            *decompressionCache
	cacheKey         string
}

// Stat implements fs.File.
func (pf *packFile) Stat() (fs.FileInfo, error) {
	return &packFileInfo{
		name:  pf.name,
		size:  int64(pf.entry.Size),
		mode:  fs.FileMode(pf.entry.Mode),
		mtime: time.Unix(pf.entry.ModTime, 0),
	}, nil
}

// Read implements fs.File.
func (pf *packFile) Read(p []byte) (n int, err error) {
	if pf.offset >= int64(pf.entry.Size) {
		return 0, io.EOF
	}

	toRead := uint64(len(p))
	remaining := pf.entry.Size - uint64(pf.offset)
	if toRead > remaining {
		toRead = remaining
	}

	if pf.entry.Compressed && pf.decompressedData == nil {
		// Check cache first.
		if pf.cache != nil {
			if cached, ok := pf.cache.Get(pf.cacheKey); ok {
				pf.decompressedData = cached
			}
		}

		// Decompress if not cached.
		if pf.decompressedData == nil {
			var compressedData []byte
			if len(pf.entry.Location.Chunks) == 0 {
				frameIdx := pf.entry.Location.FrameIndex
				frameInfo, err := pf.reader.getDataFrameInfo(frameIdx)
				if err != nil {
					return 0, err
				}

				compressedData, err = pf.reader.ReadFrameData(frameInfo, pf.entry.Location.Offset, pf.entry.CompressedSize)
				if err != nil {
					return 0, err
				}
			} else {
				var err error
				compressedData, err = pf.readChunked(0, pf.entry.CompressedSize)
				if err != nil {
					return 0, err
				}
			}

			decompressed, err := decompressFileData(compressedData)
			if err != nil {
				return 0, errDecompress(err)
			}
			pf.decompressedData = decompressed

			// Store in cache for future Opens of the same file.
			if pf.cache != nil {
				pf.cache.Add(pf.cacheKey, decompressed)
			}
		}
	}

	var data []byte
	switch {
	case pf.entry.Compressed:
		start := uint64(pf.offset)
		end := start + toRead
		if end > uint64(len(pf.decompressedData)) {
			end = uint64(len(pf.decompressedData))
		}
		data = pf.decompressedData[start:end]
	case len(pf.entry.Location.Chunks) == 0:
		frameIdx := pf.entry.Location.FrameIndex
		frameInfo, err := pf.reader.getDataFrameInfo(frameIdx)
		if err != nil {
			return 0, err
		}

		data, err = pf.reader.ReadFrameData(frameInfo, pf.entry.Location.Offset+uint64(pf.offset), toRead)
		if err != nil {
			return 0, err
		}
	default:
		data, err = pf.readChunked(uint64(pf.offset), toRead)
		if err != nil {
			return 0, err
		}
	}

	n = copy(p, data)
	pf.offset += int64(n)

	if pf.offset >= int64(pf.entry.Size) {
		err = io.EOF
	}

	return n, err
}

// Seek implements io.Seeker.
func (pf *packFile) Seek(offset int64, whence int) (int64, error) {
	var newOffset int64
	switch whence {
	case io.SeekStart:
		newOffset = offset
	case io.SeekCurrent:
		newOffset = pf.offset + offset
	case io.SeekEnd:
		newOffset = int64(pf.entry.Size) + offset
	default:
		return 0, errInvalidWhence(whence)
	}

	if newOffset < 0 {
		return 0, ErrNegativePosition
	}

	pf.offset = newOffset
	return newOffset, nil
}

// readChunked reads from chunked file.
func (pf *packFile) readChunked(offset, size uint64) ([]byte, error) {
	return readChunkedData(pf.entry.Location.Chunks, offset, size, pf.reader)
}

// Close implements fs.File.
func (pf *packFile) Close() error {
	return nil
}

// packDir implements fs.File for directories.
type packDir struct {
	name     string
	children []string
	pfs      *packFS
	offset   int
}

// Stat implements fs.File.
func (pd *packDir) Stat() (fs.FileInfo, error) {
	return &packFileInfo{
		name:  pd.name,
		mode:  fs.ModeDir | 0755,
		isDir: true,
	}, nil
}

// Read implements fs.File.
func (pd *packDir) Read(_ []byte) (n int, err error) {
	return 0, &fs.PathError{Op: "read", Path: pd.name, Err: ErrIsDirectory}
}

// Close implements fs.File.
func (pd *packDir) Close() error {
	return nil
}

// ReadDir implements fs.ReadDirFile.
func (pd *packDir) ReadDir(n int) ([]fs.DirEntry, error) {
	if pd.offset >= len(pd.children) {
		if n <= 0 {
			return nil, nil
		}
		return nil, io.EOF
	}

	end := len(pd.children)
	if n > 0 && pd.offset+n < end {
		end = pd.offset + n
	}

	entries := make([]fs.DirEntry, 0, end-pd.offset)
	for i := pd.offset; i < end; i++ {
		childName := pd.children[i]
		var dirPath string
		if pd.name == "" || pd.name == "." {
			dirPath = childName
		} else {
			dirPath = path.Join(pd.name, childName)
		}

		if _, isDir := pd.pfs.tree.Dirs[dirPath]; isDir {
			entries = append(entries, &packDirEntry{
				name:  childName,
				isDir: true,
			})
		} else if fileEntry, ok := pd.pfs.tree.Files[dirPath]; ok {
			entries = append(entries, &packDirEntry{
				name:  childName,
				size:  int64(fileEntry.Size),
				mode:  fs.FileMode(fileEntry.Mode),
				mtime: time.Unix(fileEntry.ModTime, 0),
			})
		}
	}

	pd.offset = end
	return entries, nil
}

// packFileInfo implements fs.FileInfo.
type packFileInfo struct {
	name  string
	size  int64
	mode  fs.FileMode
	mtime time.Time
	isDir bool
}

func (pfi *packFileInfo) Name() string       { return pfi.name }
func (pfi *packFileInfo) Size() int64        { return pfi.size }
func (pfi *packFileInfo) Mode() fs.FileMode  { return pfi.mode }
func (pfi *packFileInfo) ModTime() time.Time { return pfi.mtime }
func (pfi *packFileInfo) IsDir() bool        { return pfi.isDir }
func (pfi *packFileInfo) Sys() interface{}   { return nil }

// packDirEntry implements fs.DirEntry.
type packDirEntry struct {
	name  string
	isDir bool
	size  int64
	mode  fs.FileMode
	mtime time.Time
}

func (pde *packDirEntry) Name() string      { return pde.name }
func (pde *packDirEntry) IsDir() bool       { return pde.isDir }
func (pde *packDirEntry) Type() fs.FileMode { return pde.mode.Type() }

func (pde *packDirEntry) Info() (fs.FileInfo, error) {
	return &packFileInfo{
		name:  pde.name,
		size:  pde.size,
		mode:  pde.mode,
		mtime: pde.mtime,
		isDir: pde.isDir,
	}, nil
}

// decompressFileData decompresses per-file compressed data.
func decompressFileData(compressed []byte) ([]byte, error) {
	return decompressZstd(compressed)
}

// readChunkedData reads data from chunks.
func readChunkedData(chunks []ChunkInfo, offset, size uint64, reader *Reader) ([]byte, error) {
	result := make([]byte, size)
	resultOffset := uint64(0)
	remaining := size

	for _, chunk := range chunks {
		chunkEnd := chunk.Offset + uint64(chunk.Size)
		if offset >= chunkEnd {
			continue
		}
		if chunk.Offset > offset+size {
			break
		}

		chunkReadOffset := uint64(0)
		if offset > chunk.Offset {
			chunkReadOffset = offset - chunk.Offset
		}

		chunkReadSize := uint64(chunk.Size) - chunkReadOffset
		if chunkReadSize > remaining {
			chunkReadSize = remaining
		}

		frameInfo, err := reader.getDataFrameInfo(chunk.FrameIndex)
		if err != nil {
			return nil, err
		}

		data, err := reader.ReadFrameData(frameInfo, chunk.FrameOffset+chunkReadOffset, chunkReadSize)
		if err != nil {
			return nil, err
		}

		copy(result[resultOffset:], data)
		resultOffset += chunkReadSize
		remaining -= chunkReadSize
		offset += chunkReadSize

		if remaining == 0 {
			break
		}
	}

	if remaining != 0 {
		return nil, errFrameOutOfBounds(fmt.Sprintf("chunked read missing %d bytes", remaining))
	}

	return result, nil
}
