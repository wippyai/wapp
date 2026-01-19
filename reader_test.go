package wapp

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"testing"
	"testing/fstest"
)

func TestReaderHeader(t *testing.T) {
	fsys := fstest.MapFS{
		"f.txt": &fstest.MapFile{Data: []byte("x"), Mode: 0644},
	}

	var buf bytes.Buffer
	writer := NewWriter()
	_ = writer.Pack(Metadata{}, nil, fsys, NewID("test", "header"), nil, &buf)

	reader, _ := NewReader(bytes.NewReader(buf.Bytes()))
	header := reader.Header()

	if header == nil {
		t.Fatal("Header should not be nil")
	}

	if string(header.Magic[:]) != Magic {
		t.Errorf("Magic = %q, want %q", header.Magic[:], Magic)
	}

	if header.Version != Version1 {
		t.Errorf("Version = %d, want %d", header.Version, Version1)
	}

	if header.DataOffset != HeaderSize {
		t.Errorf("DataOffset = %d, want %d", header.DataOffset, HeaderSize)
	}
}

func TestGetMetadataLazy(t *testing.T) {
	fsys := fstest.MapFS{
		"file.txt": &fstest.MapFile{Data: []byte("content"), Mode: 0644},
	}

	var buf bytes.Buffer
	writer := NewWriter()
	_ = writer.Pack(Metadata{"key": "value"}, nil, fsys, NewID("test", "lazy"), nil, &buf)

	reader, _ := NewReader(bytes.NewReader(buf.Bytes()))

	meta1, err1 := reader.GetMetadata()
	meta2, err2 := reader.GetMetadata()

	if err1 != nil || err2 != nil {
		t.Fatalf("GetMetadata errors: %v, %v", err1, err2)
	}

	if meta1["key"] != meta2["key"] {
		t.Error("Metadata should be consistent across calls")
	}
}

func TestGetEntriesLazy(t *testing.T) {
	entries := []Entry{
		{ID: NewID("ns", "e1"), Kind: "test"},
	}

	var buf bytes.Buffer
	writer := NewWriter()
	_ = writer.PackEntries(Metadata{}, entries, &buf)

	reader, _ := NewReader(bytes.NewReader(buf.Bytes()))

	e1, err1 := reader.GetEntries()
	e2, err2 := reader.GetEntries()

	if err1 != nil || err2 != nil {
		t.Fatalf("GetEntries errors: %v, %v", err1, err2)
	}

	if len(e1) != len(e2) {
		t.Error("Entries should be consistent across calls")
	}
}

func TestGetFSNonExistent(t *testing.T) {
	var buf bytes.Buffer
	writer := NewWriter()
	_ = writer.PackEntries(Metadata{}, nil, &buf)

	reader, _ := NewReader(bytes.NewReader(buf.Bytes()))

	_, err := reader.GetFS(NewID("no", "such"))
	if err == nil {
		t.Error("Expected error for non-existent resource")
	}
	if !errors.Is(err, ErrResourceNotFound) {
		t.Errorf("Expected ErrResourceNotFound, got %v", err)
	}
}

func TestListResources(t *testing.T) {
	fs1 := fstest.MapFS{"a.txt": &fstest.MapFile{Data: []byte("a"), Mode: 0644}}
	fs2 := fstest.MapFS{"b.txt": &fstest.MapFile{Data: []byte("b"), Mode: 0644}}

	resources := []ResourceSpec{
		{ID: NewID("res", "one"), Meta: Metadata{"idx": 1}, FS: fs1},
		{ID: NewID("res", "two"), Meta: Metadata{"idx": 2}, FS: fs2},
	}

	var buf bytes.Buffer
	writer := NewWriter()
	_ = writer.PackWithResources(Metadata{}, nil, resources, &buf)

	reader, _ := NewReader(bytes.NewReader(buf.Bytes()))
	resList := reader.ListResources()

	if len(resList) != 2 {
		t.Fatalf("ListResources returned %d, want 2", len(resList))
	}

	for _, res := range resList {
		if res.Type != ResourceTypeTree {
			t.Errorf("Resource type = %q, want %s", res.Type, ResourceTypeTree)
		}
		if res.FileCount != 1 {
			t.Errorf("FileCount = %d, want 1", res.FileCount)
		}
		if res.Hash == "" {
			t.Error("Hash should not be empty")
		}
	}
}

func TestResourceCaching(t *testing.T) {
	fsys := fstest.MapFS{
		"file.txt": &fstest.MapFile{Data: []byte("content"), Mode: 0644},
	}

	var buf bytes.Buffer
	writer := NewWriter()
	_ = writer.Pack(Metadata{}, nil, fsys, NewID("test", "cache"), nil, &buf)

	reader, _ := NewReader(bytes.NewReader(buf.Bytes()))

	fs1, err1 := reader.GetFS(NewID("test", "cache"))
	fs2, err2 := reader.GetFS(NewID("test", "cache"))

	if err1 != nil || err2 != nil {
		t.Fatalf("GetFS errors: %v, %v", err1, err2)
	}

	f1, _ := fs1.Open("file.txt")
	d1, _ := io.ReadAll(f1)
	f1.Close()

	f2, _ := fs2.Open("file.txt")
	d2, _ := io.ReadAll(f2)
	f2.Close()

	if !bytes.Equal(d1, d2) {
		t.Error("Data mismatch between cached loads")
	}
}

func TestUnsupportedVersion(t *testing.T) {
	fsys := fstest.MapFS{
		"f.txt": &fstest.MapFile{Data: []byte("x"), Mode: 0644},
	}

	var buf bytes.Buffer
	writer := NewWriter()
	_ = writer.Pack(Metadata{}, nil, fsys, NewID("test", "ver"), nil, &buf)

	data := buf.Bytes()
	data[9] = 0xFF

	_, err := NewReader(bytes.NewReader(data))
	if err == nil {
		t.Error("Expected error for unsupported version")
	}
	if !errors.Is(err, ErrUnsupportedVersion) {
		t.Errorf("Expected ErrUnsupportedVersion, got %v", err)
	}
}

func TestDataSizeExceedsMax(t *testing.T) {
	fsys := fstest.MapFS{
		"f.txt": &fstest.MapFile{Data: []byte("x"), Mode: 0644},
	}

	var buf bytes.Buffer
	writer := NewWriter()
	_ = writer.Pack(Metadata{}, nil, fsys, NewID("test", "size"), nil, &buf)

	data := buf.Bytes()
	data[20] = 0xFF
	data[21] = 0xFF
	data[22] = 0xFF
	data[23] = 0xFF

	_, err := NewReader(bytes.NewReader(data))
	if err == nil {
		t.Error("Expected error for data size exceeds max")
	}
	if !errors.Is(err, ErrDataSizeExceeded) {
		t.Errorf("Expected ErrDataSizeExceeded, got %v", err)
	}
}

func TestCorruptedData(t *testing.T) {
	fsys := fstest.MapFS{
		"file.txt": &fstest.MapFile{Data: []byte("test"), Mode: 0644},
	}

	var buf bytes.Buffer
	writer := NewWriter()
	_ = writer.Pack(Metadata{}, nil, fsys, NewID("test", "corrupt"), nil, &buf)

	data := buf.Bytes()
	data[HeaderSize+10] ^= 0xFF

	_, err := NewReader(bytes.NewReader(data))
	if !errors.Is(err, ErrDataCorrupted) {
		t.Errorf("Expected ErrDataCorrupted, got %v", err)
	}
}

func TestTruncatedFile(t *testing.T) {
	fsys := fstest.MapFS{
		"file.txt": &fstest.MapFile{Data: []byte("content"), Mode: 0644},
	}

	var buf bytes.Buffer
	writer := NewWriter()
	_ = writer.Pack(Metadata{}, nil, fsys, NewID("test", "foot"), nil, &buf)

	data := buf.Bytes()[:10]

	_, err := NewReader(bytes.NewReader(data))
	if err == nil {
		t.Error("Expected error for truncated file")
	}
}

func TestStreamingHashValidation(t *testing.T) {
	largeContent := bytes.Repeat([]byte("X"), 100*1024)
	fsys := fstest.MapFS{
		"large.txt": &fstest.MapFile{Data: largeContent, Mode: 0644},
	}

	var buf bytes.Buffer
	writer := NewWriter()
	if err := writer.Pack(Metadata{}, nil, fsys, NewID("test", "stream"), nil, &buf); err != nil {
		t.Fatalf("Pack failed: %v", err)
	}

	reader, err := NewReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("NewReader failed: %v", err)
	}

	packFS, _ := reader.GetFS(NewID("test", "stream"))
	file, _ := packFS.Open("large.txt")
	readContent, _ := io.ReadAll(file)
	file.Close()

	if !bytes.Equal(readContent, largeContent) {
		t.Error("Content mismatch after streaming hash validation")
	}
}

func TestInvalidTOCBounds(t *testing.T) {
	var buf bytes.Buffer
	writer := NewWriter()
	if err := writer.PackEntries(Metadata{"name": "test"}, []Entry{{ID: NewID("t", "e"), Kind: "test"}}, &buf); err != nil {
		t.Fatalf("PackEntries failed: %v", err)
	}

	data := buf.Bytes()
	header, err := ReadHeader(bytes.NewReader(data[:HeaderSize]))
	if err != nil {
		t.Fatalf("ReadHeader failed: %v", err)
	}

	footerOffset := len(data) - FooterSize
	tocOffset := binary.LittleEndian.Uint64(data[footerOffset:])
	tocSize := binary.LittleEndian.Uint64(data[footerOffset+8:])

	t.Run("TOCOffsetBeforeDataEnd", func(t *testing.T) {
		corrupted := append([]byte(nil), data...)
		dataEnd := header.DataOffset + header.DataSize
		if dataEnd == 0 {
			t.Fatal("unexpected data end")
		}
		binary.LittleEndian.PutUint64(corrupted[footerOffset:], dataEnd-1)
		binary.LittleEndian.PutUint64(corrupted[footerOffset+8:], tocSize)

		_, err := NewReader(bytes.NewReader(corrupted))
		if !errors.Is(err, ErrInvalidTOC) {
			t.Errorf("Expected ErrInvalidTOC, got %v", err)
		}
	})

	t.Run("TOCSizeBeyondFooter", func(t *testing.T) {
		corrupted := append([]byte(nil), data...)
		footerStart := uint64(len(corrupted)) - FooterSize
		if tocOffset >= footerStart {
			t.Fatalf("unexpected toc offset %d >= footer start %d", tocOffset, footerStart)
		}
		tooLarge := footerStart - tocOffset + 1
		binary.LittleEndian.PutUint64(corrupted[footerOffset:], tocOffset)
		binary.LittleEndian.PutUint64(corrupted[footerOffset+8:], tooLarge)

		_, err := NewReader(bytes.NewReader(corrupted))
		if !errors.Is(err, ErrInvalidTOC) {
			t.Errorf("Expected ErrInvalidTOC, got %v", err)
		}
	})
}

func TestConcurrentReads(t *testing.T) {
	fsys := fstest.MapFS{
		"a.txt": &fstest.MapFile{Data: []byte("content-a"), Mode: 0644},
		"b.txt": &fstest.MapFile{Data: []byte("content-b"), Mode: 0644},
		"c.txt": &fstest.MapFile{Data: []byte("content-c"), Mode: 0644},
	}

	var buf bytes.Buffer
	writer := NewWriter()
	_ = writer.Pack(Metadata{}, nil, fsys, NewID("test", "concurrent"), nil, &buf)

	reader, _ := NewReader(bytes.NewReader(buf.Bytes()))
	packFS, _ := reader.GetFS(NewID("test", "concurrent"))

	done := make(chan bool, 30)
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 10; j++ {
				for _, name := range []string{"a.txt", "b.txt", "c.txt"} {
					file, _ := packFS.Open(name)
					io.ReadAll(file)
					file.Close()
				}
			}
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}
}

func TestDecompressionCacheLimit(t *testing.T) {
	fsys := fstest.MapFS{
		"cached.txt": &fstest.MapFile{Data: []byte("cached content"), Mode: 0644},
	}

	var buf bytes.Buffer
	writer := NewWriter()
	if err := writer.Pack(Metadata{}, nil, fsys, NewID("test", "cache"), nil, &buf); err != nil {
		t.Fatalf("Pack failed: %v", err)
	}

	reader, err := NewReaderWithOptions(bytes.NewReader(buf.Bytes()), WithDecompressionCacheLimit(1))
	if err != nil {
		t.Fatalf("NewReader failed: %v", err)
	}

	packFS, err := reader.GetFS(NewID("test", "cache"))
	if err != nil {
		t.Fatalf("GetFS failed: %v", err)
	}

	for i := 0; i < 2; i++ {
		file, err := packFS.Open("cached.txt")
		if err != nil {
			t.Fatalf("Open cached.txt failed: %v", err)
		}
		data, err := io.ReadAll(file)
		file.Close()
		if err != nil {
			t.Fatalf("Read cached.txt failed: %v", err)
		}
		if string(data) != "cached content" {
			t.Errorf("Content = %q, want %q", data, "cached content")
		}
	}
}
