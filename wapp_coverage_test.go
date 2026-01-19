package wapp

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

func TestFileInfoMethods(t *testing.T) {
	fsys := fstest.MapFS{
		"test.txt": &fstest.MapFile{Data: []byte("content"), Mode: 0644},
	}

	var buf bytes.Buffer
	writer := NewWriter()
	_ = writer.Pack(Metadata{}, nil, fsys, NewID("test", "info"), nil, &buf)

	reader, _ := NewReader(bytes.NewReader(buf.Bytes()))
	packFS, _ := reader.GetFS(NewID("test", "info"))

	file, err := packFS.Open("test.txt")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}

	t.Run("Name", func(t *testing.T) {
		if info.Name() != "test.txt" {
			t.Errorf("Name = %q, want test.txt", info.Name())
		}
	})

	t.Run("Size", func(t *testing.T) {
		if info.Size() != 7 {
			t.Errorf("Size = %d, want 7", info.Size())
		}
	})

	t.Run("Mode", func(t *testing.T) {
		mode := info.Mode()
		if mode&0644 != 0644 {
			t.Errorf("Mode = %o, want 0644", mode)
		}
	})

	t.Run("ModTime", func(t *testing.T) {
		// ModTime may be zero for fstest.MapFS files without explicit ModTime
		_ = info.ModTime()
	})

	t.Run("IsDir", func(t *testing.T) {
		if info.IsDir() {
			t.Error("IsDir should be false for file")
		}
	})

	t.Run("Sys", func(t *testing.T) {
		if info.Sys() != nil {
			t.Error("Sys should return nil")
		}
	})
}

func TestDirEntryMethods(t *testing.T) {
	fsys := fstest.MapFS{
		"dir/file.txt":       &fstest.MapFile{Data: []byte("x"), Mode: 0644},
		"dir/sub/nested.txt": &fstest.MapFile{Data: []byte("n"), Mode: 0644},
	}

	var buf bytes.Buffer
	writer := NewWriter()
	_ = writer.Pack(Metadata{}, nil, fsys, NewID("test", "direntry"), nil, &buf)

	reader, _ := NewReader(bytes.NewReader(buf.Bytes()))
	packFS, _ := reader.GetFS(NewID("test", "direntry"))

	entries, err := packFS.ReadDir("dir")
	if err != nil {
		t.Fatalf("ReadDir failed: %v", err)
	}

	for _, entry := range entries {
		t.Run("Name_"+entry.Name(), func(t *testing.T) {
			if entry.Name() == "" {
				t.Error("Name should not be empty")
			}
		})

		t.Run("IsDir_"+entry.Name(), func(t *testing.T) {
			isDir := entry.IsDir()
			if entry.Name() == "sub" && !isDir {
				t.Error("sub should be a directory")
			}
			if entry.Name() == "file.txt" && isDir {
				t.Error("file.txt should not be a directory")
			}
		})

		t.Run("Type_"+entry.Name(), func(t *testing.T) {
			_ = entry.Type()
		})

		t.Run("Info_"+entry.Name(), func(t *testing.T) {
			info, err := entry.Info()
			if err != nil {
				t.Errorf("Info failed: %v", err)
			}
			if info.Name() != entry.Name() {
				t.Errorf("Info.Name = %q, want %q", info.Name(), entry.Name())
			}
		})
	}
}

func TestDirectoryReadError(t *testing.T) {
	fsys := fstest.MapFS{
		"dir/file.txt": &fstest.MapFile{Data: []byte("x"), Mode: 0644},
	}

	var buf bytes.Buffer
	writer := NewWriter()
	_ = writer.Pack(Metadata{}, nil, fsys, NewID("test", "direrr"), nil, &buf)

	reader, _ := NewReader(bytes.NewReader(buf.Bytes()))
	packFS, _ := reader.GetFS(NewID("test", "direrr"))

	dir, err := packFS.Open("dir")
	if err != nil {
		t.Fatalf("Open dir failed: %v", err)
	}
	defer dir.Close()

	// Reading from a directory should fail
	buf2 := make([]byte, 10)
	_, err = dir.(io.Reader).Read(buf2)
	if err == nil {
		t.Error("Reading directory should fail")
	}
	if !errors.Is(err, ErrIsDirectory) {
		t.Errorf("Expected ErrIsDirectory, got %v", err)
	}
}

func TestFileSeekEdgeCases(t *testing.T) {
	content := []byte("0123456789")
	fsys := fstest.MapFS{
		"file.txt": &fstest.MapFile{Data: content, Mode: 0644},
	}

	var buf bytes.Buffer
	writer := NewWriter()
	_ = writer.Pack(Metadata{}, nil, fsys, NewID("test", "seek"), nil, &buf)

	reader, _ := NewReader(bytes.NewReader(buf.Bytes()))
	packFS, _ := reader.GetFS(NewID("test", "seek"))

	file, _ := packFS.Open("file.txt")
	defer file.Close()
	seeker := file.(io.Seeker)

	t.Run("SeekStart", func(t *testing.T) {
		pos, err := seeker.Seek(5, io.SeekStart)
		if err != nil || pos != 5 {
			t.Errorf("SeekStart failed: pos=%d, err=%v", pos, err)
		}
	})

	t.Run("SeekCurrent", func(t *testing.T) {
		seeker.Seek(5, io.SeekStart)
		pos, err := seeker.Seek(2, io.SeekCurrent)
		if err != nil || pos != 7 {
			t.Errorf("SeekCurrent failed: pos=%d, err=%v", pos, err)
		}
	})

	t.Run("SeekEnd", func(t *testing.T) {
		pos, err := seeker.Seek(-3, io.SeekEnd)
		if err != nil || pos != 7 {
			t.Errorf("SeekEnd failed: pos=%d, err=%v", pos, err)
		}
	})

	t.Run("SeekNegativeResult", func(t *testing.T) {
		_, err := seeker.Seek(-100, io.SeekStart)
		if !errors.Is(err, ErrNegativePosition) {
			t.Errorf("Expected ErrNegativePosition, got %v", err)
		}
	})

	t.Run("SeekInvalidWhence", func(t *testing.T) {
		_, err := seeker.Seek(0, 99)
		if !errors.Is(err, ErrInvalidWhence) {
			t.Errorf("Expected ErrInvalidWhence, got %v", err)
		}
	})

	t.Run("SeekBeyondEnd", func(t *testing.T) {
		pos, err := seeker.Seek(100, io.SeekStart)
		if err != nil {
			t.Errorf("Seek beyond end should succeed: %v", err)
		}
		if pos != 100 {
			t.Errorf("Position = %d, want 100", pos)
		}

		// Read should return EOF
		buf := make([]byte, 10)
		n, err := file.(io.Reader).Read(buf)
		if n != 0 || err != io.EOF {
			t.Errorf("Read beyond end: n=%d, err=%v", n, err)
		}
	})
}

func TestChunkedFileReading(t *testing.T) {
	// Create a file larger than ChunkSize (1MB) to trigger chunking
	largeContent := bytes.Repeat([]byte("ABCDEFGHIJ"), 150000) // 1.5MB
	fsys := fstest.MapFS{
		"large.bin": &fstest.MapFile{Data: largeContent, Mode: 0644},
	}

	var buf bytes.Buffer
	writer := NewWriter()
	if err := writer.Pack(Metadata{}, nil, fsys, NewID("test", "chunked"), nil, &buf); err != nil {
		t.Fatalf("Pack failed: %v", err)
	}

	reader, err := NewReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("NewReader failed: %v", err)
	}

	packFS, err := reader.GetFS(NewID("test", "chunked"))
	if err != nil {
		t.Fatalf("GetFS failed: %v", err)
	}

	file, err := packFS.Open("large.bin")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer file.Close()

	t.Run("FullRead", func(t *testing.T) {
		readContent, err := io.ReadAll(file)
		if err != nil {
			t.Fatalf("ReadAll failed: %v", err)
		}
		if !bytes.Equal(readContent, largeContent) {
			t.Error("Content mismatch")
		}
	})

	t.Run("SeekAndPartialRead", func(t *testing.T) {
		seeker := file.(io.Seeker)
		seeker.Seek(1000000, io.SeekStart) // Seek to 1MB

		buf := make([]byte, 1000)
		n, err := file.(io.Reader).Read(buf)
		if err != nil && err != io.EOF {
			t.Errorf("Read failed: %v", err)
		}
		if n != 1000 {
			t.Errorf("Read %d bytes, want 1000", n)
		}

		expected := largeContent[1000000 : 1000000+1000]
		if !bytes.Equal(buf[:n], expected) {
			t.Error("Partial read content mismatch")
		}
	})
}

func TestUncompressedFileReading(t *testing.T) {
	// PNG files are not compressed by the writer
	pngHeader := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}
	fsys := fstest.MapFS{
		"image.png": &fstest.MapFile{Data: pngHeader, Mode: 0644},
	}

	var buf bytes.Buffer
	writer := NewWriter()
	_ = writer.Pack(Metadata{}, nil, fsys, NewID("test", "uncomp"), nil, &buf)

	reader, _ := NewReader(bytes.NewReader(buf.Bytes()))
	packFS, _ := reader.GetFS(NewID("test", "uncomp"))

	file, err := packFS.Open("image.png")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer file.Close()

	readContent, err := io.ReadAll(file)
	if err != nil {
		t.Fatalf("ReadAll failed: %v", err)
	}

	if !bytes.Equal(readContent, pngHeader) {
		t.Error("Uncompressed content mismatch")
	}
}

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

func TestStreamingHashValidation(t *testing.T) {
	// Create content larger than hashChunkSize (32KB) to trigger streaming
	largeContent := bytes.Repeat([]byte("X"), 100*1024) // 100KB
	fsys := fstest.MapFS{
		"large.txt": &fstest.MapFile{Data: largeContent, Mode: 0644},
	}

	var buf bytes.Buffer
	writer := NewWriter()
	if err := writer.Pack(Metadata{}, nil, fsys, NewID("test", "stream"), nil, &buf); err != nil {
		t.Fatalf("Pack failed: %v", err)
	}

	// Verify we can read it back (hash validation happens in NewReader)
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

func TestErrorTypes(t *testing.T) {
	t.Run("ReadError", func(t *testing.T) {
		err := &ReadError{Op: "test", Path: "/path", Err: io.EOF}
		if !strings.Contains(err.Error(), "test") {
			t.Error("Error should contain operation")
		}
		if !strings.Contains(err.Error(), "/path") {
			t.Error("Error should contain path")
		}
		if !errors.Is(err, io.EOF) {
			t.Error("Should unwrap to original error")
		}
	})

	t.Run("ReadErrorNoPath", func(t *testing.T) {
		err := &ReadError{Op: "test", Err: io.EOF}
		if strings.Contains(err.Error(), "  ") {
			t.Error("Should not have double space when no path")
		}
	})

	t.Run("WriteError", func(t *testing.T) {
		err := &WriteError{Op: "write", Path: "file.txt", Err: io.ErrShortWrite}
		if !strings.Contains(err.Error(), "write") {
			t.Error("Error should contain operation")
		}
		if !errors.Is(err, io.ErrShortWrite) {
			t.Error("Should unwrap to original error")
		}
	})

	t.Run("FormatError", func(t *testing.T) {
		err := &FormatError{Op: "validate", Detail: "bad data", Err: ErrDataCorrupted}
		if !strings.Contains(err.Error(), "validate") {
			t.Error("Error should contain operation")
		}
		if !strings.Contains(err.Error(), "bad data") {
			t.Error("Error should contain detail")
		}
		if !errors.Is(err, ErrDataCorrupted) {
			t.Error("Should unwrap to original error")
		}
	})

	t.Run("FormatErrorNoDetail", func(t *testing.T) {
		err := &FormatError{Op: "test", Err: ErrDataCorrupted}
		errStr := err.Error()
		if !strings.Contains(errStr, "test") {
			t.Error("Error should contain operation")
		}
	})

	t.Run("FormatErrorNoErr", func(t *testing.T) {
		err := &FormatError{Op: "test"}
		errStr := err.Error()
		if !strings.Contains(errStr, "test") {
			t.Error("Error should contain operation")
		}
	})
}

func TestUnsupportedVersion(t *testing.T) {
	fsys := fstest.MapFS{
		"f.txt": &fstest.MapFile{Data: []byte("x"), Mode: 0644},
	}

	var buf bytes.Buffer
	writer := NewWriter()
	_ = writer.Pack(Metadata{}, nil, fsys, NewID("test", "ver"), nil, &buf)

	// Corrupt version byte
	data := buf.Bytes()
	data[9] = 0xFF // Version byte is at offset 9

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

	// Set data size to exceed max (1GB)
	data := buf.Bytes()
	// DataSize is at offset 20 (after magic[9] + version[1] + flags[2] + dataOffset[8])
	// Set it to a huge value
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

func TestReadDirPartial(t *testing.T) {
	fsys := fstest.MapFS{
		"dir/a.txt": &fstest.MapFile{Data: []byte("a"), Mode: 0644},
		"dir/b.txt": &fstest.MapFile{Data: []byte("b"), Mode: 0644},
		"dir/c.txt": &fstest.MapFile{Data: []byte("c"), Mode: 0644},
	}

	var buf bytes.Buffer
	writer := NewWriter()
	_ = writer.Pack(Metadata{}, nil, fsys, NewID("test", "partialdir"), nil, &buf)

	reader, _ := NewReader(bytes.NewReader(buf.Bytes()))
	packFS, _ := reader.GetFS(NewID("test", "partialdir"))

	dir, _ := packFS.Open("dir")
	defer dir.Close()
	readDir := dir.(fs.ReadDirFile)

	// Read 1 at a time
	e1, err := readDir.ReadDir(1)
	if err != nil || len(e1) != 1 {
		t.Errorf("ReadDir(1) failed: len=%d, err=%v", len(e1), err)
	}

	e2, err := readDir.ReadDir(1)
	if err != nil || len(e2) != 1 {
		t.Errorf("ReadDir(1) again failed: len=%d, err=%v", len(e2), err)
	}

	e3, err := readDir.ReadDir(1)
	if err != nil || len(e3) != 1 {
		t.Errorf("ReadDir(1) third failed: len=%d, err=%v", len(e3), err)
	}

	// Should return EOF on next read with n > 0
	e4, err := readDir.ReadDir(1)
	if err != io.EOF || len(e4) != 0 {
		t.Errorf("ReadDir(1) at end: len=%d, err=%v (expected EOF)", len(e4), err)
	}
}

func TestEmptyFile(t *testing.T) {
	fsys := fstest.MapFS{
		"empty.txt": &fstest.MapFile{Data: []byte{}, Mode: 0644},
	}

	var buf bytes.Buffer
	writer := NewWriter()
	_ = writer.Pack(Metadata{}, nil, fsys, NewID("test", "empty"), nil, &buf)

	reader, _ := NewReader(bytes.NewReader(buf.Bytes()))
	packFS, _ := reader.GetFS(NewID("test", "empty"))

	file, err := packFS.Open("empty.txt")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer file.Close()

	content, err := io.ReadAll(file)
	if err != nil {
		t.Fatalf("ReadAll failed: %v", err)
	}

	if len(content) != 0 {
		t.Errorf("Empty file should have 0 bytes, got %d", len(content))
	}

	info, _ := file.Stat()
	if info.Size() != 0 {
		t.Errorf("Empty file size should be 0, got %d", info.Size())
	}
}

func TestDecompressionCacheReuse(t *testing.T) {
	content := bytes.Repeat([]byte("cached"), 1000)
	fsys := fstest.MapFS{
		"cached.txt": &fstest.MapFile{Data: content, Mode: 0644},
	}

	var buf bytes.Buffer
	writer := NewWriter()
	_ = writer.Pack(Metadata{}, nil, fsys, NewID("test", "cache"), nil, &buf)

	reader, _ := NewReader(bytes.NewReader(buf.Bytes()))
	packFS, _ := reader.GetFS(NewID("test", "cache"))

	// First read
	file1, _ := packFS.Open("cached.txt")
	content1, _ := io.ReadAll(file1)
	file1.Close()

	// Second read - should use cache
	file2, _ := packFS.Open("cached.txt")
	content2, _ := io.ReadAll(file2)
	file2.Close()

	if !bytes.Equal(content1, content2) {
		t.Error("Cached reads should return same content")
	}

	if !bytes.Equal(content1, content) {
		t.Error("Content mismatch")
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

func TestReadDirRoot(t *testing.T) {
	fsys := fstest.MapFS{
		"root.txt":    &fstest.MapFile{Data: []byte("root"), Mode: 0644},
		"dir/sub.txt": &fstest.MapFile{Data: []byte("sub"), Mode: 0644},
	}

	var buf bytes.Buffer
	writer := NewWriter()
	_ = writer.Pack(Metadata{}, nil, fsys, NewID("test", "root"), nil, &buf)

	reader, _ := NewReader(bytes.NewReader(buf.Bytes()))
	packFS, _ := reader.GetFS(NewID("test", "root"))

	entries, err := packFS.ReadDir(".")
	if err != nil {
		t.Fatalf("ReadDir(.) failed: %v", err)
	}

	if len(entries) < 2 {
		t.Errorf("Root should have at least 2 entries, got %d", len(entries))
	}

	names := make(map[string]bool)
	for _, e := range entries {
		names[e.Name()] = true
	}

	if !names["root.txt"] {
		t.Error("Missing root.txt")
	}
	if !names["dir"] {
		t.Error("Missing dir")
	}
}

func TestOpenInvalidPath(t *testing.T) {
	fsys := fstest.MapFS{
		"file.txt": &fstest.MapFile{Data: []byte("content"), Mode: 0644},
	}

	var buf bytes.Buffer
	writer := NewWriter()
	_ = writer.Pack(Metadata{}, nil, fsys, NewID("test", "path"), nil, &buf)

	reader, _ := NewReader(bytes.NewReader(buf.Bytes()))
	packFS, _ := reader.GetFS(NewID("test", "path"))

	_, err := packFS.Open("../invalid")
	if err == nil {
		t.Error("Expected error for invalid path")
	}
}

func TestReadDirInvalidPath(t *testing.T) {
	fsys := fstest.MapFS{
		"file.txt": &fstest.MapFile{Data: []byte("content"), Mode: 0644},
	}

	var buf bytes.Buffer
	writer := NewWriter()
	_ = writer.Pack(Metadata{}, nil, fsys, NewID("test", "rdpath"), nil, &buf)

	reader, _ := NewReader(bytes.NewReader(buf.Bytes()))
	packFS, _ := reader.GetFS(NewID("test", "rdpath"))

	_, err := packFS.ReadDir("../invalid")
	if err == nil {
		t.Error("Expected error for invalid path")
	}
}

func TestReadDirNotExist(t *testing.T) {
	fsys := fstest.MapFS{
		"file.txt": &fstest.MapFile{Data: []byte("content"), Mode: 0644},
	}

	var buf bytes.Buffer
	writer := NewWriter()
	_ = writer.Pack(Metadata{}, nil, fsys, NewID("test", "notexist"), nil, &buf)

	reader, _ := NewReader(bytes.NewReader(buf.Bytes()))
	packFS, _ := reader.GetFS(NewID("test", "notexist"))

	_, err := packFS.ReadDir("nonexistent")
	if err == nil {
		t.Error("Expected error for non-existent directory")
	}
}

func TestPackWithMultipleResourcesCoverage(t *testing.T) {
	fsys1 := fstest.MapFS{
		"a.txt": &fstest.MapFile{Data: []byte("resource1"), Mode: 0644},
	}
	fsys2 := fstest.MapFS{
		"b.txt": &fstest.MapFile{Data: []byte("resource2"), Mode: 0644},
	}

	resources := []ResourceSpec{
		{ID: NewID("test", "res1"), FS: fsys1, Meta: Metadata{"name": "res1"}},
		{ID: NewID("test", "res2"), FS: fsys2, Meta: Metadata{"name": "res2"}},
	}

	var buf bytes.Buffer
	writer := NewWriter()
	err := writer.PackWithResources(Metadata{"app": "test"}, nil, resources, &buf)
	if err != nil {
		t.Fatalf("PackWithResources failed: %v", err)
	}

	reader, err := NewReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("NewReader failed: %v", err)
	}

	fs1, err := reader.GetFS(NewID("test", "res1"))
	if err != nil {
		t.Fatalf("GetFS res1 failed: %v", err)
	}

	fs2, err := reader.GetFS(NewID("test", "res2"))
	if err != nil {
		t.Fatalf("GetFS res2 failed: %v", err)
	}

	f1, _ := fs1.Open("a.txt")
	data1, _ := io.ReadAll(f1)
	f1.Close()
	if string(data1) != "resource1" {
		t.Errorf("res1 content = %q, want %q", data1, "resource1")
	}

	f2, _ := fs2.Open("b.txt")
	data2, _ := io.ReadAll(f2)
	f2.Close()
	if string(data2) != "resource2" {
		t.Errorf("res2 content = %q, want %q", data2, "resource2")
	}
}

func TestInvalidHeaderMagic(t *testing.T) {
	data := make([]byte, 300)
	copy(data, []byte("BADM")) // wrong magic

	_, err := NewReader(bytes.NewReader(data))
	if err == nil {
		t.Error("Expected error for invalid magic")
	}
}

func TestTruncatedFile(t *testing.T) {
	fsys := fstest.MapFS{
		"file.txt": &fstest.MapFile{Data: []byte("content"), Mode: 0644},
	}

	var buf bytes.Buffer
	writer := NewWriter()
	_ = writer.Pack(Metadata{}, nil, fsys, NewID("test", "foot"), nil, &buf)

	// truncate to less than footer size
	data := buf.Bytes()[:10]

	_, err := NewReader(bytes.NewReader(data))
	if err == nil {
		t.Error("Expected error for truncated file")
	}
}

func TestFileReadEOFBoundary(t *testing.T) {
	content := []byte("exactly16bytes!!")
	fsys := fstest.MapFS{
		"file.txt": &fstest.MapFile{Data: content, Mode: 0644},
	}

	var buf bytes.Buffer
	writer := NewWriter()
	_ = writer.Pack(Metadata{}, nil, fsys, NewID("test", "eof"), nil, &buf)

	reader, _ := NewReader(bytes.NewReader(buf.Bytes()))
	packFS, _ := reader.GetFS(NewID("test", "eof"))

	file, _ := packFS.Open("file.txt")

	// read full content
	data := make([]byte, 16)
	n, err := file.Read(data)
	if n != 16 {
		t.Errorf("First read n = %d, want 16", n)
	}
	if err != io.EOF {
		t.Errorf("First read err = %v, want io.EOF", err)
	}

	// read again should return 0, EOF
	n, err = file.Read(data)
	if n != 0 || err != io.EOF {
		t.Errorf("Second read: n=%d err=%v, want 0, io.EOF", n, err)
	}

	file.Close()
}

func TestDirReadProgress(t *testing.T) {
	fsys := fstest.MapFS{
		"a.txt": &fstest.MapFile{Data: []byte("a"), Mode: 0644},
		"b.txt": &fstest.MapFile{Data: []byte("b"), Mode: 0644},
		"c.txt": &fstest.MapFile{Data: []byte("c"), Mode: 0644},
	}

	var buf bytes.Buffer
	writer := NewWriter()
	_ = writer.Pack(Metadata{}, nil, fsys, NewID("test", "progress"), nil, &buf)

	reader, _ := NewReader(bytes.NewReader(buf.Bytes()))
	packFS, _ := reader.GetFS(NewID("test", "progress"))

	dir, _ := packFS.Open(".")
	readDirFile, ok := dir.(fs.ReadDirFile)
	if !ok {
		t.Fatal("Expected ReadDirFile interface")
	}

	// Read one at a time
	entries1, _ := readDirFile.ReadDir(1)
	if len(entries1) != 1 {
		t.Errorf("First ReadDir got %d entries, want 1", len(entries1))
	}

	entries2, _ := readDirFile.ReadDir(1)
	if len(entries2) != 1 {
		t.Errorf("Second ReadDir got %d entries, want 1", len(entries2))
	}

	entries3, _ := readDirFile.ReadDir(1)
	if len(entries3) != 1 {
		t.Errorf("Third ReadDir got %d entries, want 1", len(entries3))
	}

	// Read past end
	entries4, err := readDirFile.ReadDir(1)
	if len(entries4) != 0 || err != io.EOF {
		t.Errorf("Fourth ReadDir: got %d entries, err=%v, want 0 entries and io.EOF", len(entries4), err)
	}

	dir.Close()
}

func TestDirectoryInfo(t *testing.T) {
	fsys := fstest.MapFS{
		"dir/file.txt": &fstest.MapFile{Data: []byte("content"), Mode: 0644},
	}

	var buf bytes.Buffer
	writer := NewWriter()
	_ = writer.Pack(Metadata{}, nil, fsys, NewID("test", "dirinfo"), nil, &buf)

	reader, _ := NewReader(bytes.NewReader(buf.Bytes()))
	packFS, _ := reader.GetFS(NewID("test", "dirinfo"))

	dir, _ := packFS.Open("dir")
	info, err := dir.Stat()
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}

	if !info.IsDir() {
		t.Error("Expected IsDir to be true")
	}
	if info.Name() != "dir" {
		t.Errorf("Name = %q, want %q", info.Name(), "dir")
	}
	if info.Mode()&fs.ModeDir == 0 {
		t.Error("Expected ModeDir flag")
	}

	dir.Close()
}

func TestReaderHeaderAccessor(t *testing.T) {
	fsys := fstest.MapFS{
		"file.txt": &fstest.MapFile{Data: []byte("content"), Mode: 0644},
	}

	var buf bytes.Buffer
	writer := NewWriter()
	_ = writer.Pack(Metadata{}, nil, fsys, NewID("test", "size"), nil, &buf)

	reader, _ := NewReader(bytes.NewReader(buf.Bytes()))
	header := reader.Header()

	if header == nil {
		t.Fatal("Header should not be nil")
	}
	if string(header.Magic[:]) != Magic {
		t.Errorf("Header magic = %q, want %q", string(header.Magic[:]), Magic)
	}
	if header.Version != Version1 {
		t.Errorf("Header version = %d, want %d", header.Version, Version1)
	}
}

func TestHashValidationWithRead(t *testing.T) {
	content := bytes.Repeat([]byte("test"), 1024)
	fsys := fstest.MapFS{
		"file.txt": &fstest.MapFile{Data: content, Mode: 0644},
	}

	var buf bytes.Buffer
	writer := NewWriter()
	_ = writer.Pack(Metadata{}, nil, fsys, NewID("test", "hashval"), nil, &buf)

	reader, _ := NewReader(bytes.NewReader(buf.Bytes()))
	packFS, _ := reader.GetFS(NewID("test", "hashval"))

	file, _ := packFS.Open("file.txt")
	data, err := io.ReadAll(file)
	file.Close()

	if err != nil && err != io.EOF {
		t.Fatalf("Read failed: %v", err)
	}

	if !bytes.Equal(data, content) {
		t.Error("Content mismatch")
	}
}

func TestEntryWithData(t *testing.T) {
	entries := []Entry{
		{
			ID:   NewID("test", "entry"),
			Kind: "data.entry",
			Meta: Metadata{"key": "value"},
			Data: []byte("binary data"),
		},
	}

	var buf bytes.Buffer
	writer := NewWriter()
	_ = writer.PackEntries(Metadata{}, entries, &buf)

	reader, _ := NewReader(bytes.NewReader(buf.Bytes()))
	readEntries, err := reader.GetEntries()
	if err != nil {
		t.Fatalf("GetEntries failed: %v", err)
	}

	if len(readEntries) != 1 {
		t.Fatalf("Expected 1 entry, got %d", len(readEntries))
	}

	var dataBytes []byte
	switch d := readEntries[0].Data.(type) {
	case []byte:
		dataBytes = d
	case string:
		dataBytes = []byte(d)
	default:
		t.Fatalf("Entry data should be []byte or string, got %T", readEntries[0].Data)
	}
	if !bytes.Equal(dataBytes, []byte("binary data")) {
		t.Errorf("Entry data = %v, want %v", dataBytes, []byte("binary data"))
	}
}

func TestWithCompressionFunc(t *testing.T) {
	fsys := fstest.MapFS{
		"file.txt": &fstest.MapFile{Data: bytes.Repeat([]byte("x"), 1000), Mode: 0644},
		"file.bin": &fstest.MapFile{Data: bytes.Repeat([]byte("y"), 1000), Mode: 0644},
	}

	// compress only .txt
	var buf bytes.Buffer
	writer := NewWriter(WithCompressionFunc(func(name string) bool {
		return len(name) > 4 && name[len(name)-4:] == ".txt"
	}))

	err := writer.Pack(Metadata{}, nil, fsys, NewID("test", "compfunc"), nil, &buf)
	if err != nil {
		t.Fatalf("Pack failed: %v", err)
	}

	reader, _ := NewReader(bytes.NewReader(buf.Bytes()))
	packFS, _ := reader.GetFS(NewID("test", "compfunc"))

	// verify both files readable
	f1, _ := packFS.Open("file.txt")
	d1, _ := io.ReadAll(f1)
	f1.Close()
	if len(d1) != 1000 {
		t.Errorf("file.txt size = %d, want 1000", len(d1))
	}

	f2, _ := packFS.Open("file.bin")
	d2, _ := io.ReadAll(f2)
	f2.Close()
	if len(d2) != 1000 {
		t.Errorf("file.bin size = %d, want 1000", len(d2))
	}
}

func TestProgressCallbackCoverage(t *testing.T) {
	fsys := fstest.MapFS{
		"a.txt": &fstest.MapFile{Data: []byte("a"), Mode: 0644},
		"b.txt": &fstest.MapFile{Data: []byte("b"), Mode: 0644},
	}

	var calls []int
	var buf bytes.Buffer
	writer := NewWriter(WithProgressCallback(func(id ID, current, total int) {
		calls = append(calls, current)
	}))

	err := writer.Pack(Metadata{}, nil, fsys, NewID("test", "progress"), nil, &buf)
	if err != nil {
		t.Fatalf("Pack failed: %v", err)
	}

	if len(calls) == 0 {
		t.Error("Progress callback was never called")
	}
}

func TestShortHeaderData(t *testing.T) {
	data := []byte("WAPP")
	_, err := NewReader(bytes.NewReader(data))
	if err == nil {
		t.Error("Expected error for short data")
	}
}

func TestLargeUncompressibleData(t *testing.T) {
	// Create random-like uncompressible data larger than hashChunkSize
	rng := make([]byte, 64*1024) // 64KB of "random" data
	for i := range rng {
		rng[i] = byte(i*17 + i/256)
	}

	fsys := fstest.MapFS{
		"random.bin": &fstest.MapFile{Data: rng, Mode: 0644},
	}

	var buf bytes.Buffer
	writer := NewWriter(WithCompressionFunc(func(name string) bool {
		return false // disable compression
	}))
	if err := writer.Pack(Metadata{}, nil, fsys, NewID("test", "rng"), nil, &buf); err != nil {
		t.Fatalf("Pack failed: %v", err)
	}

	reader, err := NewReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("NewReader failed: %v", err)
	}

	packFS, _ := reader.GetFS(NewID("test", "rng"))
	file, _ := packFS.Open("random.bin")
	readContent, _ := io.ReadAll(file)
	file.Close()

	if !bytes.Equal(readContent, rng) {
		t.Error("Content mismatch")
	}
}

func TestIDEmptyNamespace(t *testing.T) {
	id := NewID("", "name")
	if id.String() != "name" {
		t.Errorf("String() = %q, want %q", id.String(), "name")
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

	// Call multiple times - should use cached value
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

	// Call multiple times
	e1, err1 := reader.GetEntries()
	e2, err2 := reader.GetEntries()

	if err1 != nil || err2 != nil {
		t.Fatalf("GetEntries errors: %v, %v", err1, err2)
	}

	if len(e1) != len(e2) {
		t.Error("Entries should be consistent across calls")
	}
}

func TestSeekEndAndBack(t *testing.T) {
	content := []byte("0123456789")
	fsys := fstest.MapFS{
		"file.txt": &fstest.MapFile{Data: content, Mode: 0644},
	}

	var buf bytes.Buffer
	writer := NewWriter()
	_ = writer.Pack(Metadata{}, nil, fsys, NewID("test", "seekend"), nil, &buf)

	reader, _ := NewReader(bytes.NewReader(buf.Bytes()))
	packFS, _ := reader.GetFS(NewID("test", "seekend"))

	file, _ := packFS.Open("file.txt")
	seeker := file.(io.Seeker)

	// Seek to end
	pos, err := seeker.Seek(0, io.SeekEnd)
	if err != nil {
		t.Fatalf("Seek to end failed: %v", err)
	}
	if pos != 10 {
		t.Errorf("Position = %d, want 10", pos)
	}

	// Seek back with negative offset
	pos, err = seeker.Seek(-5, io.SeekEnd)
	if err != nil {
		t.Fatalf("Seek -5 from end failed: %v", err)
	}
	if pos != 5 {
		t.Errorf("Position = %d, want 5", pos)
	}

	// Read remaining
	data := make([]byte, 10)
	n, _ := file.Read(data)
	if string(data[:n]) != "56789" {
		t.Errorf("Read = %q, want %q", string(data[:n]), "56789")
	}

	file.Close()
}

func TestReadAfterSeekBeyondEnd(t *testing.T) {
	content := []byte("hello")
	fsys := fstest.MapFS{
		"file.txt": &fstest.MapFile{Data: content, Mode: 0644},
	}

	var buf bytes.Buffer
	writer := NewWriter()
	_ = writer.Pack(Metadata{}, nil, fsys, NewID("test", "beyond"), nil, &buf)

	reader, _ := NewReader(bytes.NewReader(buf.Bytes()))
	packFS, _ := reader.GetFS(NewID("test", "beyond"))

	file, _ := packFS.Open("file.txt")
	seeker := file.(io.Seeker)

	// Seek beyond end
	_, _ = seeker.Seek(100, io.SeekStart)

	// Read should return EOF
	data := make([]byte, 10)
	n, err := file.Read(data)
	if n != 0 || err != io.EOF {
		t.Errorf("Read after seek beyond: n=%d, err=%v, want 0, io.EOF", n, err)
	}

	file.Close()
}

func TestCompressedFilePartialReads(t *testing.T) {
	// Compressible content
	content := bytes.Repeat([]byte("hello world "), 100)
	fsys := fstest.MapFS{
		"file.txt": &fstest.MapFile{Data: content, Mode: 0644},
	}

	var buf bytes.Buffer
	writer := NewWriter()
	_ = writer.Pack(Metadata{}, nil, fsys, NewID("test", "partial"), nil, &buf)

	reader, _ := NewReader(bytes.NewReader(buf.Bytes()))
	packFS, _ := reader.GetFS(NewID("test", "partial"))

	file, _ := packFS.Open("file.txt")

	// Read in small chunks
	var result []byte
	chunk := make([]byte, 50)
	for {
		n, err := file.Read(chunk)
		result = append(result, chunk[:n]...)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Read error: %v", err)
		}
	}

	if !bytes.Equal(result, content) {
		t.Error("Partial reads result mismatch")
	}

	file.Close()
}

func TestNestedDirectories(t *testing.T) {
	fsys := fstest.MapFS{
		"a/b/c/file.txt": &fstest.MapFile{Data: []byte("deep"), Mode: 0644},
	}

	var buf bytes.Buffer
	writer := NewWriter()
	_ = writer.Pack(Metadata{}, nil, fsys, NewID("test", "nested"), nil, &buf)

	reader, _ := NewReader(bytes.NewReader(buf.Bytes()))
	packFS, _ := reader.GetFS(NewID("test", "nested"))

	// Verify we can read the deep file
	file, err := packFS.Open("a/b/c/file.txt")
	if err != nil {
		t.Fatalf("Open nested file failed: %v", err)
	}
	data, _ := io.ReadAll(file)
	file.Close()

	if string(data) != "deep" {
		t.Errorf("Content = %q, want %q", string(data), "deep")
	}

	// Verify directory listing at each level
	dirs := []string{".", "a", "a/b", "a/b/c"}
	for _, dir := range dirs {
		entries, err := packFS.ReadDir(dir)
		if err != nil {
			t.Errorf("ReadDir(%q) failed: %v", dir, err)
		}
		if len(entries) == 0 {
			t.Errorf("ReadDir(%q) returned empty", dir)
		}
	}
}

func TestReadDirAllAtOnce(t *testing.T) {
	fsys := fstest.MapFS{
		"a.txt": &fstest.MapFile{Data: []byte("a"), Mode: 0644},
		"b.txt": &fstest.MapFile{Data: []byte("b"), Mode: 0644},
		"c.txt": &fstest.MapFile{Data: []byte("c"), Mode: 0644},
	}

	var buf bytes.Buffer
	writer := NewWriter()
	_ = writer.Pack(Metadata{}, nil, fsys, NewID("test", "all"), nil, &buf)

	reader, _ := NewReader(bytes.NewReader(buf.Bytes()))
	packFS, _ := reader.GetFS(NewID("test", "all"))

	dir, _ := packFS.Open(".")
	readDirFile := dir.(fs.ReadDirFile)

	// Read all with n = 0
	entries, err := readDirFile.ReadDir(0)
	if err != nil {
		t.Fatalf("ReadDir(0) failed: %v", err)
	}
	if len(entries) != 3 {
		t.Errorf("Got %d entries, want 3", len(entries))
	}

	// Reading again with n <= 0 should return empty (all consumed)
	entries2, err2 := readDirFile.ReadDir(-1)
	if err2 != nil {
		t.Fatalf("ReadDir(-1) failed: %v", err2)
	}
	if len(entries2) != 0 {
		t.Errorf("Second ReadDir got %d entries, want 0", len(entries2))
	}

	dir.Close()
}

func TestLargeChunkedFile(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping large file test in short mode")
	}

	// Create 1.5MB of uncompressible data
	size := 1536 * 1024
	data := make([]byte, size)
	for i := range data {
		data[i] = byte(i*17 + i/256)
	}

	fsys := fstest.MapFS{
		"large.bin": &fstest.MapFile{Data: data, Mode: 0644},
	}

	var buf bytes.Buffer
	writer := NewWriter(WithCompressionFunc(func(name string) bool {
		return false // disable compression to keep size
	}))
	if err := writer.Pack(Metadata{}, nil, fsys, NewID("test", "chunked"), nil, &buf); err != nil {
		t.Fatalf("Pack failed: %v", err)
	}

	reader, err := NewReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("NewReader failed: %v", err)
	}

	packFS, err := reader.GetFS(NewID("test", "chunked"))
	if err != nil {
		t.Fatalf("GetFS failed: %v", err)
	}

	file, err := packFS.Open("large.bin")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	readData, err := io.ReadAll(file)
	file.Close()
	if err != nil && err != io.EOF {
		t.Fatalf("ReadAll failed: %v", err)
	}

	if len(readData) != size {
		t.Errorf("Read %d bytes, want %d", len(readData), size)
	}

	if !bytes.Equal(readData, data) {
		t.Error("Data mismatch")
	}
}

func TestChunkedFileSeek(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping large file test in short mode")
	}

	// Create 1.5MB of uncompressible data
	size := 1536 * 1024
	data := make([]byte, size)
	for i := range data {
		data[i] = byte(i*17 + i/256)
	}

	fsys := fstest.MapFS{
		"large.bin": &fstest.MapFile{Data: data, Mode: 0644},
	}

	var buf bytes.Buffer
	writer := NewWriter(WithCompressionFunc(func(name string) bool {
		return false
	}))
	_ = writer.Pack(Metadata{}, nil, fsys, NewID("test", "chunkedseek"), nil, &buf)

	reader, _ := NewReader(bytes.NewReader(buf.Bytes()))
	packFS, _ := reader.GetFS(NewID("test", "chunkedseek"))

	file, _ := packFS.Open("large.bin")
	seeker := file.(io.Seeker)

	// Seek to middle of file (into second chunk)
	seekPos := int64(1024*1024 + 512) // 1MB + 512 bytes
	pos, err := seeker.Seek(seekPos, io.SeekStart)
	if err != nil {
		t.Fatalf("Seek failed: %v", err)
	}
	if pos != seekPos {
		t.Errorf("Seek returned %d, want %d", pos, seekPos)
	}

	// Read some data
	chunk := make([]byte, 1024)
	n, err := file.Read(chunk)
	if err != nil && err != io.EOF {
		t.Fatalf("Read failed: %v", err)
	}

	expected := data[seekPos : seekPos+int64(n)]
	if !bytes.Equal(chunk[:n], expected) {
		t.Error("Data mismatch after seek")
	}

	file.Close()
}

func TestResourceCaching(t *testing.T) {
	fsys := fstest.MapFS{
		"file.txt": &fstest.MapFile{Data: []byte("content"), Mode: 0644},
	}

	var buf bytes.Buffer
	writer := NewWriter()
	_ = writer.Pack(Metadata{}, nil, fsys, NewID("test", "cache"), nil, &buf)

	reader, _ := NewReader(bytes.NewReader(buf.Bytes()))

	// Load same resource multiple times
	fs1, err1 := reader.GetFS(NewID("test", "cache"))
	fs2, err2 := reader.GetFS(NewID("test", "cache"))

	if err1 != nil || err2 != nil {
		t.Fatalf("GetFS errors: %v, %v", err1, err2)
	}

	// Both should work
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

// Custom FS that fails on file read
type errorReadFS struct {
	files map[string][]byte
}

func (e *errorReadFS) Open(name string) (fs.File, error) {
	if name == "." {
		return &errorReadDir{files: e.files}, nil
	}
	if _, ok := e.files[name]; ok {
		return &errorReadFile{name: name}, nil
	}
	return nil, fs.ErrNotExist
}

type errorReadDir struct {
	files map[string][]byte
}

func (e *errorReadDir) Read([]byte) (int, error) { return 0, fs.ErrInvalid }
func (e *errorReadDir) Close() error             { return nil }
func (e *errorReadDir) Stat() (fs.FileInfo, error) {
	return &simpleFileInfo{name: ".", isDir: true}, nil
}
func (e *errorReadDir) ReadDir(n int) ([]fs.DirEntry, error) {
	var entries []fs.DirEntry
	for name := range e.files {
		entries = append(entries, &simpleDirEntry{name: name})
	}
	return entries, nil
}

type errorReadFile struct {
	name string
}

func (e *errorReadFile) Read([]byte) (int, error)   { return 0, errors.New("read error") }
func (e *errorReadFile) Close() error               { return nil }
func (e *errorReadFile) Stat() (fs.FileInfo, error) { return &simpleFileInfo{name: e.name}, nil }

type simpleFileInfo struct {
	name  string
	isDir bool
}

func (s *simpleFileInfo) Name() string       { return s.name }
func (s *simpleFileInfo) Size() int64        { return 100 }
func (s *simpleFileInfo) Mode() fs.FileMode  { return 0644 }
func (s *simpleFileInfo) ModTime() time.Time { return time.Time{} }
func (s *simpleFileInfo) IsDir() bool        { return s.isDir }
func (s *simpleFileInfo) Sys() interface{}   { return nil }

type simpleDirEntry struct {
	name string
}

func (s *simpleDirEntry) Name() string               { return s.name }
func (s *simpleDirEntry) IsDir() bool                { return false }
func (s *simpleDirEntry) Type() fs.FileMode          { return 0644 }
func (s *simpleDirEntry) Info() (fs.FileInfo, error) { return &simpleFileInfo{name: s.name}, nil }

func TestPackWithReadError(t *testing.T) {
	fsys := &errorReadFS{files: map[string][]byte{"file.txt": {}}}

	var buf bytes.Buffer
	writer := NewWriter()
	err := writer.Pack(Metadata{}, nil, fsys, NewID("test", "readfail"), nil, &buf)
	if err == nil {
		t.Error("Expected error from read failure")
	}
}

func TestOpenNonExistent(t *testing.T) {
	fsys := fstest.MapFS{
		"exists.txt": &fstest.MapFile{Data: []byte("content"), Mode: 0644},
	}

	var buf bytes.Buffer
	writer := NewWriter()
	_ = writer.Pack(Metadata{}, nil, fsys, NewID("test", "nonexist"), nil, &buf)

	reader, _ := NewReader(bytes.NewReader(buf.Bytes()))
	packFS, _ := reader.GetFS(NewID("test", "nonexist"))

	_, err := packFS.Open("nothere.txt")
	if err == nil {
		t.Error("Expected error for non-existent file")
	}
}

func TestReadDirectoryAsFile(t *testing.T) {
	fsys := fstest.MapFS{
		"dir/file.txt": &fstest.MapFile{Data: []byte("content"), Mode: 0644},
	}

	var buf bytes.Buffer
	writer := NewWriter()
	_ = writer.Pack(Metadata{}, nil, fsys, NewID("test", "dirread"), nil, &buf)

	reader, _ := NewReader(bytes.NewReader(buf.Bytes()))
	packFS, _ := reader.GetFS(NewID("test", "dirread"))

	dir, err := packFS.Open("dir")
	if err != nil {
		t.Fatalf("Open dir failed: %v", err)
	}

	// Try to read the directory
	buf2 := make([]byte, 10)
	_, err = dir.Read(buf2)
	if err == nil {
		t.Error("Expected error reading directory")
	}
	dir.Close()
}

func TestErrorUnwrap(t *testing.T) {
	origErr := errors.New("original")

	readErr := &ReadError{Op: "test", Path: "/file", Err: origErr}
	if !errors.Is(readErr, origErr) {
		t.Error("ReadError should unwrap to original error")
	}

	writeErr := &WriteError{Op: "test", Path: "/file", Err: origErr}
	if !errors.Is(writeErr, origErr) {
		t.Error("WriteError should unwrap to original error")
	}

	fmtErr := &FormatError{Op: "test", Detail: "detail", Err: origErr}
	if !errors.Is(fmtErr, origErr) {
		t.Error("FormatError should unwrap to original error")
	}
}

func TestDirReadPartialMultiple(t *testing.T) {
	fsys := fstest.MapFS{
		"a.txt": &fstest.MapFile{Data: []byte("a"), Mode: 0644},
		"b.txt": &fstest.MapFile{Data: []byte("b"), Mode: 0644},
		"c.txt": &fstest.MapFile{Data: []byte("c"), Mode: 0644},
		"d.txt": &fstest.MapFile{Data: []byte("d"), Mode: 0644},
		"e.txt": &fstest.MapFile{Data: []byte("e"), Mode: 0644},
	}

	var buf bytes.Buffer
	writer := NewWriter()
	_ = writer.Pack(Metadata{}, nil, fsys, NewID("test", "dirpartial"), nil, &buf)

	reader, _ := NewReader(bytes.NewReader(buf.Bytes()))
	packFS, _ := reader.GetFS(NewID("test", "dirpartial"))

	dir, _ := packFS.Open(".")
	readDirFile := dir.(fs.ReadDirFile)

	// Read in batches
	batch1, _ := readDirFile.ReadDir(2)
	batch2, _ := readDirFile.ReadDir(2)
	batch3, _ := readDirFile.ReadDir(2) // should get remaining 1

	total := len(batch1) + len(batch2) + len(batch3)
	if total != 5 {
		t.Errorf("Total entries = %d, want 5", total)
	}

	dir.Close()
}

func TestRootDirInfo(t *testing.T) {
	fsys := fstest.MapFS{
		"file.txt": &fstest.MapFile{Data: []byte("content"), Mode: 0644},
	}

	var buf bytes.Buffer
	writer := NewWriter()
	_ = writer.Pack(Metadata{}, nil, fsys, NewID("test", "rootinfo"), nil, &buf)

	reader, _ := NewReader(bytes.NewReader(buf.Bytes()))
	packFS, _ := reader.GetFS(NewID("test", "rootinfo"))

	dir, _ := packFS.Open(".")
	info, err := dir.Stat()
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}

	if !info.IsDir() {
		t.Error("Root should be a directory")
	}

	dir.Close()
}

func TestOpenEmptyPath(t *testing.T) {
	fsys := fstest.MapFS{
		"file.txt": &fstest.MapFile{Data: []byte("content"), Mode: 0644},
	}

	var buf bytes.Buffer
	writer := NewWriter()
	_ = writer.Pack(Metadata{}, nil, fsys, NewID("test", "empty"), nil, &buf)

	reader, _ := NewReader(bytes.NewReader(buf.Bytes()))
	packFS, _ := reader.GetFS(NewID("test", "empty"))

	// Open "." should work as root
	file, err := packFS.Open(".")
	if err != nil {
		t.Errorf("Open('.') failed: %v", err)
	}
	file.Close()
}

func TestIDEquality(t *testing.T) {
	id1 := NewID("ns", "name")
	id2 := NewID("ns", "name")
	id3 := NewID("ns", "other")
	id4 := NewID("other", "name")

	if !id1.Equal(id2) {
		t.Error("Same IDs should be equal")
	}
	if id1.Equal(id3) {
		t.Error("Different names should not be equal")
	}
	if id1.Equal(id4) {
		t.Error("Different namespaces should not be equal")
	}
}

func TestVersionValidation(t *testing.T) {
	fsys := fstest.MapFS{
		"file.txt": &fstest.MapFile{Data: []byte("content"), Mode: 0644},
	}

	var buf bytes.Buffer
	writer := NewWriter()
	_ = writer.Pack(Metadata{}, nil, fsys, NewID("test", "version"), nil, &buf)

	data := buf.Bytes()
	// Change version byte
	data[9] = 0xFF // invalid version

	_, err := NewReader(bytes.NewReader(data))
	if err == nil {
		t.Error("Expected error for invalid version")
	}
}

func TestSeekCurrentMode(t *testing.T) {
	content := []byte("0123456789abcdef")
	fsys := fstest.MapFS{
		"file.txt": &fstest.MapFile{Data: content, Mode: 0644},
	}

	var buf bytes.Buffer
	writer := NewWriter()
	_ = writer.Pack(Metadata{}, nil, fsys, NewID("test", "seekcur"), nil, &buf)

	reader, _ := NewReader(bytes.NewReader(buf.Bytes()))
	packFS, _ := reader.GetFS(NewID("test", "seekcur"))

	file, _ := packFS.Open("file.txt")
	seeker := file.(io.Seeker)

	// Seek to position 5
	seeker.Seek(5, io.SeekStart)

	// Seek +3 from current
	pos, err := seeker.Seek(3, io.SeekCurrent)
	if err != nil {
		t.Fatalf("Seek failed: %v", err)
	}
	if pos != 8 {
		t.Errorf("Position = %d, want 8", pos)
	}

	// Read from position 8
	data := make([]byte, 4)
	n, _ := file.Read(data)
	if string(data[:n]) != "89ab" {
		t.Errorf("Read = %q, want %q", string(data[:n]), "89ab")
	}

	file.Close()
}
