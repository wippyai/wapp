package wapp

const (
	// ChunkSize is the default chunk size for large files (1MB).
	ChunkSize uint64 = 1024 * 1024

	// ResourceTypeTree identifies a filesystem tree resource.
	ResourceTypeTree = "tree"
)

// TreeResource represents a filesystem tree in the pack.
type TreeResource struct {
	ID   ID       `json:"ID" msgpack:"id"`
	Meta Metadata `json:"Meta" msgpack:"meta"`

	// Path index for O(1) file lookups.
	Files map[string]FileEntry `json:"Files" msgpack:"files"`

	// Directory listings for ReadDir.
	Dirs map[string][]string `json:"Dirs" msgpack:"dirs"`
}

// FileEntry describes a file in a tree resource.
type FileEntry struct {
	Size           uint64       `json:"Size" msgpack:"size"`
	CompressedSize uint64       `json:"CompressedSize,omitempty" msgpack:"compressed_size,omitempty"`
	Mode           uint32       `json:"Mode" msgpack:"mode"`
	ModTime        int64        `json:"ModTime" msgpack:"mtime"`
	Hash           string       `json:"Hash" msgpack:"hash"`
	Compressed     bool         `json:"Compressed" msgpack:"compressed"`
	Meta           Metadata     `json:"Meta,omitempty" msgpack:"meta,omitempty"`
	Location       FileLocation `json:"Location" msgpack:"location"`
}

// FileLocation describes where file content is stored.
type FileLocation struct {
	FrameIndex uint32      `json:"FrameIndex" msgpack:"frame"`
	Offset     uint64      `json:"Offset" msgpack:"offset"`
	Chunks     []ChunkInfo `json:"Chunks,omitempty" msgpack:"chunks,omitempty"`
}

// ChunkInfo describes a chunk of a large file.
type ChunkInfo struct {
	Size        uint32 `json:"Size" msgpack:"size"`
	Offset      uint64 `json:"Offset" msgpack:"offset"`
	FrameIndex  uint32 `json:"FrameIndex" msgpack:"frame"`
	FrameOffset uint64 `json:"FrameOffset" msgpack:"frame_offset"`
}
