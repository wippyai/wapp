package wapp

import (
	"testing"
)

func TestChunkSize(t *testing.T) {
	if ChunkSize != 1024*1024 {
		t.Errorf("ChunkSize = %d, want %d", ChunkSize, 1024*1024)
	}
}

func TestResourceTypeTree(t *testing.T) {
	if ResourceTypeTree != "tree" {
		t.Errorf("ResourceTypeTree = %q, want %q", ResourceTypeTree, "tree")
	}
}

func TestTreeResource(t *testing.T) {
	tree := &TreeResource{
		ID:    NewID("test", "tree"),
		Meta:  Metadata{"version": "1.0"},
		Files: make(map[string]*FileEntry),
		Dirs:  make(map[string][]string),
	}

	if tree.ID.IsZero() {
		t.Error("TreeResource ID should not be zero")
	}

	if tree.Meta["version"] != "1.0" {
		t.Error("TreeResource Meta not set correctly")
	}
}

func TestFileEntry(t *testing.T) {
	entry := FileEntry{
		Size:           1024,
		CompressedSize: 512,
		Mode:           0644,
		ModTime:        1234567890,
		Hash:           "abc123",
		Compressed:     true,
		Meta:           Metadata{"type": "text"},
		Location: FileLocation{
			FrameIndex: 3,
			Offset:     100,
		},
	}

	if entry.Size != 1024 {
		t.Errorf("Size = %d, want %d", entry.Size, 1024)
	}

	if entry.CompressedSize != 512 {
		t.Errorf("CompressedSize = %d, want %d", entry.CompressedSize, 512)
	}

	if !entry.Compressed {
		t.Error("Compressed should be true")
	}

	if entry.Location.FrameIndex != 3 {
		t.Errorf("FrameIndex = %d, want %d", entry.Location.FrameIndex, 3)
	}
}

func TestFileLocation(t *testing.T) {
	t.Run("SimpleLocation", func(t *testing.T) {
		loc := FileLocation{
			FrameIndex: 5,
			Offset:     1000,
		}

		if loc.FrameIndex != 5 {
			t.Errorf("FrameIndex = %d, want %d", loc.FrameIndex, 5)
		}

		if loc.Offset != 1000 {
			t.Errorf("Offset = %d, want %d", loc.Offset, 1000)
		}

		if len(loc.Chunks) != 0 {
			t.Error("Chunks should be empty for simple location")
		}
	})

	t.Run("ChunkedLocation", func(t *testing.T) {
		loc := FileLocation{
			FrameIndex: 5,
			Offset:     0,
			Chunks: []ChunkInfo{
				{Size: 1024, Offset: 0, FrameIndex: 5, FrameOffset: 0},
				{Size: 1024, Offset: 1024, FrameIndex: 5, FrameOffset: 1024},
				{Size: 512, Offset: 2048, FrameIndex: 6, FrameOffset: 0},
			},
		}

		if len(loc.Chunks) != 3 {
			t.Errorf("Chunks length = %d, want %d", len(loc.Chunks), 3)
		}

		if loc.Chunks[2].FrameIndex != 6 {
			t.Errorf("Third chunk FrameIndex = %d, want %d", loc.Chunks[2].FrameIndex, 6)
		}
	})
}

func TestChunkInfo(t *testing.T) {
	chunk := ChunkInfo{
		Size:        65536,
		Offset:      1048576,
		FrameIndex:  10,
		FrameOffset: 2048,
	}

	if chunk.Size != 65536 {
		t.Errorf("Size = %d, want %d", chunk.Size, 65536)
	}

	if chunk.Offset != 1048576 {
		t.Errorf("Offset = %d, want %d", chunk.Offset, 1048576)
	}

	if chunk.FrameIndex != 10 {
		t.Errorf("FrameIndex = %d, want %d", chunk.FrameIndex, 10)
	}

	if chunk.FrameOffset != 2048 {
		t.Errorf("FrameOffset = %d, want %d", chunk.FrameOffset, 2048)
	}
}
