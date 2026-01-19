package wapp

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestFileRead(t *testing.T) {
	fsys := fstest.MapFS{
		"file.txt":       &fstest.MapFile{Data: []byte("hello world"), Mode: 0644},
		"dir/nested.txt": &fstest.MapFile{Data: []byte("nested content"), Mode: 0644},
	}

	var buf bytes.Buffer
	writer := NewWriter()
	_ = writer.Pack(Metadata{}, nil, fsys, NewID("test", "read"), nil, &buf)

	reader, _ := NewReader(bytes.NewReader(buf.Bytes()))
	packFS, _ := reader.GetFS(NewID("test", "read"))

	t.Run("ReadFile", func(t *testing.T) {
		file, err := packFS.Open("file.txt")
		if err != nil {
			t.Fatalf("Open file.txt failed: %v", err)
		}
		defer file.Close()

		data, err := io.ReadAll(file)
		if err != nil {
			t.Fatalf("ReadAll failed: %v", err)
		}

		if string(data) != "hello world" {
			t.Errorf("File content = %q, want %q", data, "hello world")
		}
	})

	t.Run("ReadNestedFile", func(t *testing.T) {
		file, err := packFS.Open("dir/nested.txt")
		if err != nil {
			t.Fatalf("Open dir/nested.txt failed: %v", err)
		}
		defer file.Close()

		data, err := io.ReadAll(file)
		if err != nil {
			t.Fatalf("ReadAll failed: %v", err)
		}

		if string(data) != "nested content" {
			t.Errorf("File content = %q, want %q", data, "nested content")
		}
	})

	t.Run("NotExist", func(t *testing.T) {
		_, err := packFS.Open("nonexistent.txt")
		if err == nil {
			t.Error("Expected error for non-existent file")
		}
	})
}

func TestFileStat(t *testing.T) {
	fsys := fstest.MapFS{
		"file.txt": &fstest.MapFile{Data: []byte("content"), Mode: 0644},
	}

	var buf bytes.Buffer
	writer := NewWriter()
	_ = writer.Pack(Metadata{}, nil, fsys, NewID("test", "stat"), nil, &buf)

	reader, _ := NewReader(bytes.NewReader(buf.Bytes()))
	packFS, _ := reader.GetFS(NewID("test", "stat"))

	file, err := packFS.Open("file.txt")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}

	if info.Name() != "file.txt" {
		t.Errorf("Name = %q, want file.txt", info.Name())
	}
	if info.Size() != 7 {
		t.Errorf("Size = %d, want 7", info.Size())
	}
	if info.IsDir() {
		t.Error("IsDir should be false")
	}
	if info.Sys() != nil {
		t.Error("Sys should return nil")
	}
}

func TestFileSeek(t *testing.T) {
	content := []byte("0123456789abcdefghij")
	fsys := fstest.MapFS{
		"large.txt": &fstest.MapFile{Data: content, Mode: 0644},
	}

	var buf bytes.Buffer
	writer := NewWriter()
	_ = writer.Pack(Metadata{}, nil, fsys, NewID("test", "seek"), nil, &buf)

	reader, _ := NewReader(bytes.NewReader(buf.Bytes()))
	packFS, _ := reader.GetFS(NewID("test", "seek"))

	file, _ := packFS.Open("large.txt")
	defer file.Close()

	seeker, ok := file.(io.Seeker)
	if !ok {
		t.Fatal("File should implement io.Seeker")
	}

	buf2 := make([]byte, 5)
	n, _ := file.(io.Reader).Read(buf2)
	if string(buf2[:n]) != "01234" {
		t.Errorf("First read = %q, want '01234'", buf2[:n])
	}

	pos, err := seeker.Seek(10, io.SeekStart)
	if err != nil {
		t.Fatalf("Seek failed: %v", err)
	}
	if pos != 10 {
		t.Errorf("Seek position = %d, want 10", pos)
	}

	n, _ = file.(io.Reader).Read(buf2)
	if string(buf2[:n]) != "abcde" {
		t.Errorf("After seek read = %q, want 'abcde'", buf2[:n])
	}

	pos, err = seeker.Seek(-5, io.SeekCurrent)
	if err != nil {
		t.Fatalf("SeekCurrent failed: %v", err)
	}
	if pos != 10 {
		t.Errorf("SeekCurrent position = %d, want 10", pos)
	}

	pos, err = seeker.Seek(-5, io.SeekEnd)
	if err != nil {
		t.Fatalf("SeekEnd failed: %v", err)
	}
	if pos != 15 {
		t.Errorf("SeekEnd position = %d, want 15", pos)
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

		buf := make([]byte, 10)
		n, err := file.(io.Reader).Read(buf)
		if n != 0 || err != io.EOF {
			t.Errorf("Read beyond end: n=%d, err=%v", n, err)
		}
	})
}

func TestReadDir(t *testing.T) {
	fsys := fstest.MapFS{
		"dir/nested.txt": &fstest.MapFile{Data: []byte("n"), Mode: 0644},
		"dir/other.txt":  &fstest.MapFile{Data: []byte("o"), Mode: 0644},
	}

	var buf bytes.Buffer
	writer := NewWriter()
	_ = writer.Pack(Metadata{}, nil, fsys, NewID("test", "readdir"), nil, &buf)

	reader, _ := NewReader(bytes.NewReader(buf.Bytes()))
	packFS, _ := reader.GetFS(NewID("test", "readdir"))

	entries, err := packFS.ReadDir("dir")
	if err != nil {
		t.Fatalf("ReadDir failed: %v", err)
	}

	if len(entries) != 2 {
		t.Errorf("Dir entries = %d, want 2", len(entries))
	}

	names := make(map[string]bool)
	for _, e := range entries {
		names[e.Name()] = true
	}

	if !names["nested.txt"] || !names["other.txt"] {
		t.Errorf("Expected nested.txt and other.txt, got %v", names)
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

func TestDirReadDir(t *testing.T) {
	fsys := fstest.MapFS{
		"a/1.txt": &fstest.MapFile{Data: []byte("1"), Mode: 0644},
		"a/2.txt": &fstest.MapFile{Data: []byte("2"), Mode: 0644},
		"a/3.txt": &fstest.MapFile{Data: []byte("3"), Mode: 0644},
		"a/4.txt": &fstest.MapFile{Data: []byte("4"), Mode: 0644},
		"a/5.txt": &fstest.MapFile{Data: []byte("5"), Mode: 0644},
	}

	var buf bytes.Buffer
	writer := NewWriter()
	_ = writer.Pack(Metadata{}, nil, fsys, NewID("test", "readdir"), nil, &buf)

	reader, _ := NewReader(bytes.NewReader(buf.Bytes()))
	packFS, _ := reader.GetFS(NewID("test", "readdir"))

	dir, err := packFS.Open("a")
	if err != nil {
		t.Fatalf("Open dir failed: %v", err)
	}
	defer dir.Close()

	readDirFile, ok := dir.(fs.ReadDirFile)
	if !ok {
		t.Fatal("Directory should implement ReadDirFile")
	}

	entries1, err := readDirFile.ReadDir(2)
	if err != nil {
		t.Fatalf("ReadDir(2) failed: %v", err)
	}
	if len(entries1) != 2 {
		t.Errorf("ReadDir(2) returned %d entries, want 2", len(entries1))
	}

	entries2, err := readDirFile.ReadDir(2)
	if err != nil {
		t.Fatalf("ReadDir(2) again failed: %v", err)
	}
	if len(entries2) != 2 {
		t.Errorf("ReadDir(2) again returned %d entries, want 2", len(entries2))
	}

	entries3, err := readDirFile.ReadDir(-1)
	if err != nil {
		t.Fatalf("ReadDir(-1) failed: %v", err)
	}
	if len(entries3) != 1 {
		t.Errorf("ReadDir(-1) returned %d entries, want 1", len(entries3))
	}

	entries4, err := readDirFile.ReadDir(-1)
	if err != nil {
		t.Fatalf("ReadDir(-1) at end failed: %v", err)
	}
	if len(entries4) != 0 {
		t.Errorf("ReadDir(-1) at end returned %d entries, want 0", len(entries4))
	}
}

func TestOpenDir(t *testing.T) {
	fsys := fstest.MapFS{
		"dir/file.txt": &fstest.MapFile{Data: []byte("content"), Mode: 0644},
	}

	var buf bytes.Buffer
	writer := NewWriter()
	_ = writer.Pack(Metadata{}, nil, fsys, NewID("test", "opendir"), nil, &buf)

	reader, _ := NewReader(bytes.NewReader(buf.Bytes()))
	packFS, _ := reader.GetFS(NewID("test", "opendir"))

	dir, err := packFS.Open("dir")
	if err != nil {
		t.Fatalf("Open dir failed: %v", err)
	}
	defer dir.Close()

	info, err := dir.Stat()
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}

	if !info.IsDir() {
		t.Error("IsDir should be true")
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

	buf2 := make([]byte, 10)
	_, err = dir.(io.Reader).Read(buf2)
	if err == nil {
		t.Error("Reading directory should fail")
	}
	if !errors.Is(err, ErrIsDirectory) {
		t.Errorf("Expected ErrIsDirectory, got %v", err)
	}
}

func TestInvalidPath(t *testing.T) {
	fsys := fstest.MapFS{
		"file.txt": &fstest.MapFile{Data: []byte("test"), Mode: 0644},
	}

	var buf bytes.Buffer
	writer := NewWriter()
	_ = writer.Pack(Metadata{}, nil, fsys, NewID("test", "invalid"), nil, &buf)

	reader, _ := NewReader(bytes.NewReader(buf.Bytes()))
	packFS, _ := reader.GetFS(NewID("test", "invalid"))

	_, err := packFS.Open("../escape")
	if err == nil {
		t.Error("Expected error for path escape attempt")
	}

	_, err = packFS.Open("/absolute")
	if err == nil {
		t.Error("Expected error for absolute path")
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

func TestDeepNestedDirectories(t *testing.T) {
	fsys := fstest.MapFS{
		"a/b/c/d/e/file.txt": &fstest.MapFile{Data: []byte("deep"), Mode: 0644},
	}

	var buf bytes.Buffer
	writer := NewWriter()
	_ = writer.Pack(Metadata{}, nil, fsys, NewID("test", "deep"), nil, &buf)

	reader, _ := NewReader(bytes.NewReader(buf.Bytes()))
	packFS, _ := reader.GetFS(NewID("test", "deep"))

	file, err := packFS.Open("a/b/c/d/e/file.txt")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer file.Close()

	data, _ := io.ReadAll(file)
	if string(data) != "deep" {
		t.Errorf("Content = %q, want 'deep'", data)
	}
}

func TestCacheKeyIsolationAcrossResources(t *testing.T) {
	fs1 := fstest.MapFS{
		"same.txt": &fstest.MapFile{Data: []byte("resource-one"), Mode: 0644},
	}
	fs2 := fstest.MapFS{
		"same.txt": &fstest.MapFile{Data: []byte("resource-two"), Mode: 0644},
	}

	var buf bytes.Buffer
	writer := NewWriter()
	resources := []ResourceSpec{
		{ID: NewID("one", "res"), FS: fs1},
		{ID: NewID("two", "res"), FS: fs2},
	}
	_ = writer.PackWithResources(Metadata{}, nil, resources, &buf)

	reader, _ := NewReader(bytes.NewReader(buf.Bytes()))

	fsOne, _ := reader.GetFS(NewID("one", "res"))
	fsTwo, _ := reader.GetFS(NewID("two", "res"))

	first, _ := fsOne.Open("same.txt")
	dataOne, _ := io.ReadAll(first)
	_ = first.Close()

	second, _ := fsTwo.Open("same.txt")
	dataTwo, _ := io.ReadAll(second)
	_ = second.Close()

	if string(dataOne) != "resource-one" {
		t.Errorf("Resource one content = %q, want %q", dataOne, "resource-one")
	}
	if string(dataTwo) != "resource-two" {
		t.Errorf("Resource two content = %q, want %q", dataTwo, "resource-two")
	}
}

func TestHTTPFileServer(t *testing.T) {
	fsys := fstest.MapFS{
		"file.txt": &fstest.MapFile{Data: []byte("hello over http"), Mode: 0644},
	}

	var buf bytes.Buffer
	writer := NewWriter()
	_ = writer.Pack(Metadata{}, nil, fsys, NewID("test", "http"), nil, &buf)

	reader, _ := NewReader(bytes.NewReader(buf.Bytes()))
	packFS, _ := reader.GetFS(NewID("test", "http"))

	handler := http.FileServer(http.FS(packFS))
	req := httptest.NewRequest(http.MethodGet, "/file.txt", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	resp := rec.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	body, _ := io.ReadAll(resp.Body)
	if string(body) != "hello over http" {
		t.Errorf("Body = %q, want %q", body, "hello over http")
	}

	if contentType := resp.Header.Get("Content-Type"); !strings.HasPrefix(contentType, "text/plain") {
		t.Errorf("Content-Type = %q, want text/plain", contentType)
	}
}

func TestHTTPRangeRequest(t *testing.T) {
	content := bytes.Repeat([]byte("0123456789"), 100)
	fsys := fstest.MapFS{
		"large.txt": &fstest.MapFile{Data: content, Mode: 0644},
	}

	var buf bytes.Buffer
	writer := NewWriter()
	_ = writer.Pack(Metadata{}, nil, fsys, NewID("test", "range"), nil, &buf)

	reader, _ := NewReader(bytes.NewReader(buf.Bytes()))
	packFS, _ := reader.GetFS(NewID("test", "range"))

	handler := http.FileServer(http.FS(packFS))
	req := httptest.NewRequest(http.MethodGet, "/large.txt", nil)
	req.Header.Set("Range", "bytes=10-19")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	resp := rec.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusPartialContent {
		t.Fatalf("Status = %d, want %d", resp.StatusCode, http.StatusPartialContent)
	}

	body, _ := io.ReadAll(resp.Body)
	if string(body) != string(content[10:20]) {
		t.Errorf("Range body = %q, want %q", body, content[10:20])
	}
}
