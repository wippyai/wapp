package wapp

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"io/fs"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/hashicorp/go-msgpack/v2/codec"
)

func TestID(t *testing.T) {
	t.Run("NewID", func(t *testing.T) {
		id := NewID("ns", "name")
		if id.Namespace != "ns" || id.Name != "name" {
			t.Errorf("NewID failed: got %v", id)
		}
	})

	t.Run("String", func(t *testing.T) {
		tests := []struct {
			id       ID
			expected string
		}{
			{NewID("ns", "name"), "ns/name"},
			{NewID("", "name"), "name"},
			{NewID("org", "pkg"), "org/pkg"},
		}

		for _, tt := range tests {
			if got := tt.id.String(); got != tt.expected {
				t.Errorf("ID.String() = %q, want %q", got, tt.expected)
			}
		}
	})

	t.Run("Equal", func(t *testing.T) {
		id1 := NewID("ns", "name")
		id2 := NewID("ns", "name")
		id3 := NewID("ns", "other")

		if !id1.Equal(id2) {
			t.Error("Equal IDs should be equal")
		}
		if id1.Equal(id3) {
			t.Error("Different IDs should not be equal")
		}
	})

	t.Run("IsZero", func(t *testing.T) {
		var emptyID ID
		if !emptyID.IsZero() {
			t.Error("Empty ID should be zero")
		}
		if NewID("ns", "name").IsZero() {
			t.Error("Non-empty ID should not be zero")
		}
	})
}

func TestHeaderFooter(t *testing.T) {
	t.Run("WriteAndReadHeader", func(t *testing.T) {
		var buf bytes.Buffer

		header := &Header{
			DataOffset: HeaderSize,
			DataSize:   1000,
		}
		copy(header.DataHash[:], []byte("12345678901234567890123456789012"))

		if err := WriteHeader(&buf, header); err != nil {
			t.Fatalf("WriteHeader failed: %v", err)
		}

		if buf.Len() != HeaderSize {
			t.Errorf("Header size = %d, want %d", buf.Len(), HeaderSize)
		}

		readHeader, err := ReadHeader(bytes.NewReader(buf.Bytes()))
		if err != nil {
			t.Fatalf("ReadHeader failed: %v", err)
		}

		if string(readHeader.Magic[:]) != Magic {
			t.Errorf("Magic = %q, want %q", readHeader.Magic[:], Magic)
		}
		if readHeader.Version != Version1 {
			t.Errorf("Version = %d, want %d", readHeader.Version, Version1)
		}
		if readHeader.DataOffset != header.DataOffset {
			t.Errorf("DataOffset = %d, want %d", readHeader.DataOffset, header.DataOffset)
		}
		if readHeader.DataSize != header.DataSize {
			t.Errorf("DataSize = %d, want %d", readHeader.DataSize, header.DataSize)
		}
	})

	t.Run("InvalidMagic", func(t *testing.T) {
		buf := make([]byte, HeaderSize)
		copy(buf[:9], "INVALID!!")

		_, err := ReadHeader(bytes.NewReader(buf))
		if err == nil {
			t.Error("Expected error for invalid magic")
		}
	})
}

func TestPackEntriesRoundTrip(t *testing.T) {
	metadata := Metadata{
		"name":    "test-pack",
		"version": "1.0.0",
	}

	entries := []Entry{
		{
			ID:   NewID("test", "entry1"),
			Kind: "test.kind",
			Meta: Metadata{"key": "value"},
			Data: map[string]any{"config": "data"},
		},
		{
			ID:   NewID("test", "entry2"),
			Kind: "test.other",
			Data: "simple data",
		},
	}

	var buf bytes.Buffer
	writer := NewWriter()
	if err := writer.PackEntries(metadata, entries, &buf); err != nil {
		t.Fatalf("PackEntries failed: %v", err)
	}

	reader, err := NewReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("NewReader failed: %v", err)
	}

	readMeta, err := reader.GetMetadata()
	if err != nil {
		t.Fatalf("GetMetadata failed: %v", err)
	}

	if readMeta["name"] != "test-pack" {
		t.Errorf("Metadata name = %v, want test-pack", readMeta["name"])
	}
	if readMeta["version"] != "1.0.0" {
		t.Errorf("Metadata version = %v, want 1.0.0", readMeta["version"])
	}

	readEntries, err := reader.GetEntries()
	if err != nil {
		t.Fatalf("GetEntries failed: %v", err)
	}

	if len(readEntries) != len(entries) {
		t.Fatalf("Entries count = %d, want %d", len(readEntries), len(entries))
	}

	if !readEntries[0].ID.Equal(entries[0].ID) {
		t.Errorf("Entry[0].ID = %v, want %v", readEntries[0].ID, entries[0].ID)
	}
	if readEntries[0].Kind != entries[0].Kind {
		t.Errorf("Entry[0].Kind = %v, want %v", readEntries[0].Kind, entries[0].Kind)
	}
}

func TestPackWithFilesystem(t *testing.T) {
	fsys := fstest.MapFS{
		"file.txt":       &fstest.MapFile{Data: []byte("hello world"), Mode: 0644},
		"dir/nested.txt": &fstest.MapFile{Data: []byte("nested content"), Mode: 0644},
		"dir/other.txt":  &fstest.MapFile{Data: []byte("other file"), Mode: 0644},
		"binary.bin":     &fstest.MapFile{Data: []byte{0x00, 0x01, 0x02, 0x03}, Mode: 0644},
		"already.gz":     &fstest.MapFile{Data: []byte{0x1f, 0x8b}, Mode: 0644},
	}

	metadata := Metadata{"type": "module"}
	entries := []Entry{
		{ID: NewID("test", "files"), Kind: "fs.embed"},
	}
	resourceID := NewID("test", "files")
	resourceMeta := Metadata{"embedded": true}

	var buf bytes.Buffer
	writer := NewWriter()
	if err := writer.Pack(metadata, entries, fsys, resourceID, resourceMeta, &buf); err != nil {
		t.Fatalf("Pack failed: %v", err)
	}

	reader, err := NewReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("NewReader failed: %v", err)
	}

	resources := reader.ListResources()
	if len(resources) != 1 {
		t.Fatalf("Resources count = %d, want 1", len(resources))
	}

	if !resources[0].ID.Equal(resourceID) {
		t.Errorf("Resource ID = %v, want %v", resources[0].ID, resourceID)
	}
	if resources[0].Type != ResourceTypeTree {
		t.Errorf("Resource type = %v, want %s", resources[0].Type, ResourceTypeTree)
	}
	if resources[0].FileCount != 5 {
		t.Errorf("FileCount = %d, want 5", resources[0].FileCount)
	}

	packFS, err := reader.GetFS(resourceID)
	if err != nil {
		t.Fatalf("GetFS failed: %v", err)
	}

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

	t.Run("ReadDir", func(t *testing.T) {
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
	})

	t.Run("ReadDirRoot", func(t *testing.T) {
		entries, err := packFS.ReadDir(".")
		if err != nil {
			t.Fatalf("ReadDir root failed: %v", err)
		}

		if len(entries) < 3 {
			t.Errorf("Root entries = %d, want at least 3", len(entries))
		}
	})

	t.Run("Stat", func(t *testing.T) {
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
		if info.Size() != 11 {
			t.Errorf("Size = %d, want 11", info.Size())
		}
		if info.IsDir() {
			t.Error("IsDir should be false")
		}
	})

	t.Run("OpenDir", func(t *testing.T) {
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
	})

	t.Run("NotExist", func(t *testing.T) {
		_, err := packFS.Open("nonexistent.txt")
		if err == nil {
			t.Error("Expected error for non-existent file")
		}
	})
}

func TestPackWithMultipleResources(t *testing.T) {
	fs1 := fstest.MapFS{
		"readme.md": &fstest.MapFile{Data: []byte("# Module 1"), Mode: 0644},
	}
	fs2 := fstest.MapFS{
		"data.json": &fstest.MapFile{Data: []byte(`{"key":"value"}`), Mode: 0644},
	}

	metadata := Metadata{"multi": true}
	entries := []Entry{}
	resources := []ResourceSpec{
		{ID: NewID("mod", "docs"), Meta: Metadata{"type": "docs"}, FS: fs1},
		{ID: NewID("mod", "data"), Meta: Metadata{"type": "data"}, FS: fs2},
	}

	var buf bytes.Buffer
	writer := NewWriter()
	if err := writer.PackWithResources(metadata, entries, resources, &buf); err != nil {
		t.Fatalf("PackWithResources failed: %v", err)
	}

	reader, err := NewReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("NewReader failed: %v", err)
	}

	resList := reader.ListResources()
	if len(resList) != 2 {
		t.Fatalf("Resources count = %d, want 2", len(resList))
	}

	docsFS, err := reader.GetFS(NewID("mod", "docs"))
	if err != nil {
		t.Fatalf("GetFS docs failed: %v", err)
	}

	file, err := docsFS.Open("readme.md")
	if err != nil {
		t.Fatalf("Open readme.md failed: %v", err)
	}
	data, _ := io.ReadAll(file)
	file.Close()

	if string(data) != "# Module 1" {
		t.Errorf("readme.md content = %q, want '# Module 1'", data)
	}

	dataFS, err := reader.GetFS(NewID("mod", "data"))
	if err != nil {
		t.Fatalf("GetFS data failed: %v", err)
	}

	file, err = dataFS.Open("data.json")
	if err != nil {
		t.Fatalf("Open data.json failed: %v", err)
	}
	data, _ = io.ReadAll(file)
	file.Close()

	if string(data) != `{"key":"value"}` {
		t.Errorf("data.json content = %q", data)
	}
}

func TestFileSeek(t *testing.T) {
	fsys := fstest.MapFS{
		"large.txt": &fstest.MapFile{
			Data: []byte("0123456789abcdefghij"),
			Mode: 0644,
		},
	}

	var buf bytes.Buffer
	writer := NewWriter()
	if err := writer.Pack(Metadata{}, nil, fsys, NewID("test", "seek"), nil, &buf); err != nil {
		t.Fatalf("Pack failed: %v", err)
	}

	reader, err := NewReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("NewReader failed: %v", err)
	}

	packFS, err := reader.GetFS(NewID("test", "seek"))
	if err != nil {
		t.Fatalf("GetFS failed: %v", err)
	}

	file, err := packFS.Open("large.txt")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
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

func TestLargeFileChunking(t *testing.T) {
	largeData := make([]byte, 2*1024*1024)
	for i := range largeData {
		largeData[i] = byte(i % 256)
	}

	fsys := fstest.MapFS{
		"large.bin": &fstest.MapFile{Data: largeData, Mode: 0644},
	}

	var buf bytes.Buffer
	writer := NewWriter()
	if err := writer.Pack(Metadata{}, nil, fsys, NewID("test", "large"), nil, &buf); err != nil {
		t.Fatalf("Pack failed: %v", err)
	}

	reader, err := NewReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("NewReader failed: %v", err)
	}

	packFS, err := reader.GetFS(NewID("test", "large"))
	if err != nil {
		t.Fatalf("GetFS failed: %v", err)
	}

	file, err := packFS.Open("large.bin")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer file.Close()

	readData, err := io.ReadAll(file)
	if err != nil {
		t.Fatalf("ReadAll failed: %v", err)
	}

	if len(readData) != len(largeData) {
		t.Fatalf("Data length = %d, want %d", len(readData), len(largeData))
	}

	if !bytes.Equal(readData, largeData) {
		t.Error("Large file data mismatch")
	}
}

func TestCompressionSkip(t *testing.T) {
	fsys := fstest.MapFS{
		"script.js":     &fstest.MapFile{Data: []byte("console.log('test');"), Mode: 0644},
		"image.png":     &fstest.MapFile{Data: []byte{0x89, 0x50, 0x4e, 0x47}, Mode: 0644},
		"archive.gz":    &fstest.MapFile{Data: []byte{0x1f, 0x8b, 0x08, 0x00}, Mode: 0644},
		"document.html": &fstest.MapFile{Data: []byte("<html></html>"), Mode: 0644},
	}

	var buf bytes.Buffer
	writer := NewWriter()
	if err := writer.Pack(Metadata{}, nil, fsys, NewID("test", "compress"), nil, &buf); err != nil {
		t.Fatalf("Pack failed: %v", err)
	}

	reader, err := NewReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("NewReader failed: %v", err)
	}

	packFS, err := reader.GetFS(NewID("test", "compress"))
	if err != nil {
		t.Fatalf("GetFS failed: %v", err)
	}

	for _, name := range []string{"script.js", "image.png", "archive.gz", "document.html"} {
		file, err := packFS.Open(name)
		if err != nil {
			t.Errorf("Open %s failed: %v", name, err)
			continue
		}

		data, err := io.ReadAll(file)
		file.Close()
		if err != nil {
			t.Errorf("Read %s failed: %v", name, err)
			continue
		}

		original := fsys[name].Data
		if !bytes.Equal(data, original) {
			t.Errorf("File %s data mismatch", name)
		}
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

func TestProgressCallback(t *testing.T) {
	fsys := fstest.MapFS{
		"a.txt": &fstest.MapFile{Data: []byte("a"), Mode: 0644},
		"b.txt": &fstest.MapFile{Data: []byte("b"), Mode: 0644},
		"c.txt": &fstest.MapFile{Data: []byte("c"), Mode: 0644},
	}

	var progressCalls []struct {
		current int
		total   int
	}

	var buf bytes.Buffer
	writer := NewWriter(WithProgressCallback(func(id ID, current, total int) {
		progressCalls = append(progressCalls, struct {
			current int
			total   int
		}{current, total})
	}))

	if err := writer.Pack(Metadata{}, nil, fsys, NewID("test", "progress"), nil, &buf); err != nil {
		t.Fatalf("Pack failed: %v", err)
	}

	if len(progressCalls) != 3 {
		t.Errorf("Progress calls = %d, want 3", len(progressCalls))
	}

	for _, call := range progressCalls {
		if call.total != 3 {
			t.Errorf("Total = %d, want 3", call.total)
		}
	}
}

func TestCustomCompressionFunc(t *testing.T) {
	fsys := fstest.MapFS{
		"skip.custom":     &fstest.MapFile{Data: []byte("should not compress"), Mode: 0644},
		"compress.custom": &fstest.MapFile{Data: []byte("should compress"), Mode: 0644},
	}

	var buf bytes.Buffer
	writer := NewWriter(WithCompressionFunc(func(filename string) bool {
		return filepath.Base(filename) == "compress.custom"
	}))

	if err := writer.Pack(Metadata{}, nil, fsys, NewID("test", "custom"), nil, &buf); err != nil {
		t.Fatalf("Pack failed: %v", err)
	}

	reader, err := NewReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("NewReader failed: %v", err)
	}

	packFS, err := reader.GetFS(NewID("test", "custom"))
	if err != nil {
		t.Fatalf("GetFS failed: %v", err)
	}

	for _, name := range []string{"skip.custom", "compress.custom"} {
		file, err := packFS.Open(name)
		if err != nil {
			t.Errorf("Open %s failed: %v", name, err)
			continue
		}

		data, err := io.ReadAll(file)
		file.Close()
		if err != nil {
			t.Errorf("Read %s failed: %v", name, err)
			continue
		}

		original := fsys[name].Data
		if !bytes.Equal(data, original) {
			t.Errorf("File %s data mismatch", name)
		}
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
	if err := writer.Pack(Metadata{}, nil, fsys, NewID("test", "readdir"), nil, &buf); err != nil {
		t.Fatalf("Pack failed: %v", err)
	}

	reader, err := NewReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("NewReader failed: %v", err)
	}

	packFS, err := reader.GetFS(NewID("test", "readdir"))
	if err != nil {
		t.Fatalf("GetFS failed: %v", err)
	}

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

func TestInvalidPath(t *testing.T) {
	fsys := fstest.MapFS{
		"file.txt": &fstest.MapFile{Data: []byte("test"), Mode: 0644},
	}

	var buf bytes.Buffer
	writer := NewWriter()
	if err := writer.Pack(Metadata{}, nil, fsys, NewID("test", "invalid"), nil, &buf); err != nil {
		t.Fatalf("Pack failed: %v", err)
	}

	reader, err := NewReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("NewReader failed: %v", err)
	}

	packFS, err := reader.GetFS(NewID("test", "invalid"))
	if err != nil {
		t.Fatalf("GetFS failed: %v", err)
	}

	_, err = packFS.Open("../escape")
	if err == nil {
		t.Error("Expected error for path escape attempt")
	}

	_, err = packFS.Open("/absolute")
	if err == nil {
		t.Error("Expected error for absolute path")
	}
}

func TestEmptyPack(t *testing.T) {
	var buf bytes.Buffer
	writer := NewWriter()
	if err := writer.PackEntries(Metadata{}, nil, &buf); err != nil {
		t.Fatalf("PackEntries with nil entries failed: %v", err)
	}

	reader, err := NewReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("NewReader failed: %v", err)
	}

	entries, err := reader.GetEntries()
	if err != nil {
		t.Fatalf("GetEntries failed: %v", err)
	}

	if len(entries) != 0 {
		t.Errorf("Entries = %d, want 0", len(entries))
	}
}

func TestResourceNotFound(t *testing.T) {
	var buf bytes.Buffer
	writer := NewWriter()
	if err := writer.PackEntries(Metadata{}, nil, &buf); err != nil {
		t.Fatalf("PackEntries failed: %v", err)
	}

	reader, err := NewReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("NewReader failed: %v", err)
	}

	_, err = reader.GetFS(NewID("nonexistent", "resource"))
	if err == nil {
		t.Error("Expected error for non-existent resource")
	}
}

func BenchmarkPackSmallFiles(b *testing.B) {
	fsys := make(fstest.MapFS)
	for i := 0; i < 100; i++ {
		fsys[filepath.Join("dir", string(rune('a'+i%26))+".txt")] = &fstest.MapFile{
			Data: []byte("small file content"),
			Mode: 0644,
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var buf bytes.Buffer
		writer := NewWriter()
		_ = writer.Pack(Metadata{}, nil, fsys, NewID("bench", "small"), nil, &buf)
	}
}

func BenchmarkReadFiles(b *testing.B) {
	fsys := make(fstest.MapFS)
	for i := 0; i < 100; i++ {
		fsys[filepath.Join("dir", string(rune('a'+i%26))+".txt")] = &fstest.MapFile{
			Data: []byte("small file content for reading"),
			Mode: 0644,
		}
	}

	var buf bytes.Buffer
	writer := NewWriter()
	_ = writer.Pack(Metadata{}, nil, fsys, NewID("bench", "read"), nil, &buf)
	data := buf.Bytes()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		reader, _ := NewReader(bytes.NewReader(data))
		packFS, _ := reader.GetFS(NewID("bench", "read"))
		for name := range fsys {
			file, _ := packFS.Open(name)
			_, _ = io.ReadAll(file)
			_ = file.Close()
		}
	}
}

func TestStructuredErrors(t *testing.T) {
	t.Run("ResourceNotFound", func(t *testing.T) {
		var buf bytes.Buffer
		writer := NewWriter()
		_ = writer.PackEntries(Metadata{}, nil, &buf)

		reader, _ := NewReader(bytes.NewReader(buf.Bytes()))
		_, err := reader.GetFS(NewID("nonexistent", "resource"))

		if err == nil {
			t.Fatal("Expected error")
		}

		if !errors.Is(err, ErrResourceNotFound) {
			t.Errorf("Expected ErrResourceNotFound, got %v", err)
		}

		var readErr *ReadError
		if !errors.As(err, &readErr) {
			t.Error("Expected ReadError type")
		}

		if readErr.Path != "nonexistent/resource" {
			t.Errorf("Path = %q, want 'nonexistent/resource'", readErr.Path)
		}
	})

	t.Run("InvalidMagic", func(t *testing.T) {
		badData := make([]byte, 500)
		copy(badData[:9], "BADMAGIC!")

		_, err := NewReader(bytes.NewReader(badData))
		if err == nil {
			t.Fatal("Expected error")
		}

		if !errors.Is(err, ErrInvalidMagic) {
			t.Errorf("Expected ErrInvalidMagic, got %v", err)
		}

		var formatErr *FormatError
		if !errors.As(err, &formatErr) {
			t.Error("Expected FormatError type")
		}
	})
}

func TestFrameHashMismatch(t *testing.T) {
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

	tocStart := int(tocOffset)
	tocEnd := int(tocOffset + tocSize)
	tocCompressed := data[tocStart:tocEnd]
	tocDecompressed, err := decompressZstd(tocCompressed)
	if err != nil {
		t.Fatalf("decompress toc failed: %v", err)
	}

	toc := &TOC{}
	decoder := codec.NewDecoder(bytes.NewReader(tocDecompressed), newMsgpackHandle())
	if err := decoder.Decode(toc); err != nil {
		t.Fatalf("decode toc failed: %v", err)
	}

	originalHash := toc.Metadata.Hash
	if originalHash == "" {
		t.Fatal("expected metadata hash")
	}
	toc.Metadata.Hash = strings.Repeat("0", len(originalHash))
	if toc.Metadata.Hash == originalHash {
		toc.Metadata.Hash = strings.Repeat("1", len(originalHash))
	}

	var tocBuf bytes.Buffer
	encoder := codec.NewEncoder(&tocBuf, newMsgpackHandle())
	if err := encoder.Encode(toc); err != nil {
		t.Fatalf("encode toc failed: %v", err)
	}

	var tocCompBuf bytes.Buffer
	if err := compressZstd(tocBuf.Bytes(), &tocCompBuf); err != nil {
		t.Fatalf("compress toc failed: %v", err)
	}

	dataSection := data[HeaderSize : HeaderSize+int(header.DataSize)]
	newData := make([]byte, 0, HeaderSize+len(dataSection)+tocCompBuf.Len()+FooterSize)
	newData = append(newData, data[:HeaderSize]...)
	newData = append(newData, dataSection...)
	newData = append(newData, tocCompBuf.Bytes()...)

	newFooter := &Footer{
		TOCOffset: uint64(HeaderSize) + header.DataSize,
		TOCSize:   uint64(tocCompBuf.Len()),
	}
	var footerBuf bytes.Buffer
	if err := WriteFooter(&footerBuf, newFooter); err != nil {
		t.Fatalf("write footer failed: %v", err)
	}
	newData = append(newData, footerBuf.Bytes()...)

	reader, err := NewReader(bytes.NewReader(newData))
	if err != nil {
		t.Fatalf("NewReader failed: %v", err)
	}

	_, err = reader.GetMetadata()
	if !errors.Is(err, ErrFrameHashMismatch) {
		t.Errorf("Expected ErrFrameHashMismatch, got %v", err)
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
	if err := writer.PackWithResources(Metadata{}, nil, resources, &buf); err != nil {
		t.Fatalf("PackWithResources failed: %v", err)
	}

	reader, err := NewReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("NewReader failed: %v", err)
	}

	fsOne, err := reader.GetFS(NewID("one", "res"))
	if err != nil {
		t.Fatalf("GetFS one failed: %v", err)
	}
	fsTwo, err := reader.GetFS(NewID("two", "res"))
	if err != nil {
		t.Fatalf("GetFS two failed: %v", err)
	}

	first, err := fsOne.Open("same.txt")
	if err != nil {
		t.Fatalf("Open same.txt (one) failed: %v", err)
	}
	dataOne, err := io.ReadAll(first)
	_ = first.Close()
	if err != nil {
		t.Fatalf("ReadAll (one) failed: %v", err)
	}

	second, err := fsTwo.Open("same.txt")
	if err != nil {
		t.Fatalf("Open same.txt (two) failed: %v", err)
	}
	dataTwo, err := io.ReadAll(second)
	_ = second.Close()
	if err != nil {
		t.Fatalf("ReadAll (two) failed: %v", err)
	}

	if string(dataOne) != "resource-one" {
		t.Errorf("Resource one content = %q, want %q", dataOne, "resource-one")
	}
	if string(dataTwo) != "resource-two" {
		t.Errorf("Resource two content = %q, want %q", dataTwo, "resource-two")
	}
}

func TestInvalidTOCOffsetTooLarge(t *testing.T) {
	var buf bytes.Buffer
	writer := NewWriter()
	if err := writer.PackEntries(Metadata{"name": "test"}, []Entry{{ID: NewID("t", "e"), Kind: "test"}}, &buf); err != nil {
		t.Fatalf("PackEntries failed: %v", err)
	}

	data := buf.Bytes()
	footerOffset := len(data) - FooterSize
	binary.LittleEndian.PutUint64(data[footerOffset:], uint64(math.MaxInt64)+1)
	binary.LittleEndian.PutUint64(data[footerOffset+8:], 8)

	_, err := NewReader(bytes.NewReader(data))
	if !errors.Is(err, ErrInvalidTOC) {
		t.Errorf("Expected ErrInvalidTOC, got %v", err)
	}
}

func TestReadDirMissingChild(t *testing.T) {
	tree := &TreeResource{
		ID:    NewID("test", "broken"),
		Files: map[string]FileEntry{},
		Dirs:  map[string][]string{"": {"missing.txt"}},
	}

	pfs := newPackFS(tree, &Reader{})
	_, err := pfs.ReadDir(".")
	if !errors.Is(err, ErrInvalidTOC) {
		t.Errorf("Expected ErrInvalidTOC, got %v", err)
	}
}

func TestHTTPFileServerServeContent(t *testing.T) {
	fsys := fstest.MapFS{
		"file.txt": &fstest.MapFile{Data: []byte("hello over http"), Mode: 0644},
	}

	var buf bytes.Buffer
	writer := NewWriter()
	if err := writer.Pack(Metadata{}, nil, fsys, NewID("test", "http"), nil, &buf); err != nil {
		t.Fatalf("Pack failed: %v", err)
	}

	reader, err := NewReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("NewReader failed: %v", err)
	}

	packFS, err := reader.GetFS(NewID("test", "http"))
	if err != nil {
		t.Fatalf("GetFS failed: %v", err)
	}

	handler := http.FileServer(http.FS(packFS))
	req := httptest.NewRequest(http.MethodGet, "/file.txt", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	resp := rec.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll failed: %v", err)
	}
	if string(body) != "hello over http" {
		t.Errorf("Body = %q, want %q", body, "hello over http")
	}

	if contentType := resp.Header.Get("Content-Type"); !strings.HasPrefix(contentType, "text/plain") {
		t.Errorf("Content-Type = %q, want text/plain", contentType)
	}
}

func TestHTTPFileServerRangeRequest(t *testing.T) {
	content := bytes.Repeat([]byte("0123456789"), 100)
	fsys := fstest.MapFS{
		"large.txt": &fstest.MapFile{Data: content, Mode: 0644},
	}

	var buf bytes.Buffer
	writer := NewWriter()
	if err := writer.Pack(Metadata{}, nil, fsys, NewID("test", "range"), nil, &buf); err != nil {
		t.Fatalf("Pack failed: %v", err)
	}

	reader, err := NewReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("NewReader failed: %v", err)
	}

	packFS, err := reader.GetFS(NewID("test", "range"))
	if err != nil {
		t.Fatalf("GetFS failed: %v", err)
	}

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

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll failed: %v", err)
	}

	if string(body) != string(content[10:20]) {
		t.Errorf("Range body = %q, want %q", body, content[10:20])
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

func TestReadFrameDataBounds(t *testing.T) {
	fsys := fstest.MapFS{
		"file.txt": &fstest.MapFile{Data: []byte("frame data bounds"), Mode: 0644},
	}

	var buf bytes.Buffer
	writer := NewWriter()
	if err := writer.Pack(Metadata{}, nil, fsys, NewID("test", "bounds"), nil, &buf); err != nil {
		t.Fatalf("Pack failed: %v", err)
	}

	reader, err := NewReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("NewReader failed: %v", err)
	}

	if len(reader.toc.DataFrames) == 0 {
		t.Fatal("expected data frames")
	}

	frame := reader.toc.DataFrames[0]
	if frame.Size < 2 {
		t.Fatalf("unexpected frame size %d", frame.Size)
	}

	_, err = reader.readFrameData(frame, frame.Size-1, 2)
	if !errors.Is(err, ErrFrameOutOfBounds) {
		t.Errorf("Expected ErrFrameOutOfBounds, got %v", err)
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

func TestEmptyDirectory(t *testing.T) {
	fsys := fstest.MapFS{
		"empty/placeholder.txt": &fstest.MapFile{Data: []byte(""), Mode: 0644},
	}

	var buf bytes.Buffer
	writer := NewWriter()
	if err := writer.Pack(Metadata{}, nil, fsys, NewID("test", "empty"), nil, &buf); err != nil {
		t.Fatalf("Pack failed: %v", err)
	}

	reader, err := NewReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("NewReader failed: %v", err)
	}

	packFS, err := reader.GetFS(NewID("test", "empty"))
	if err != nil {
		t.Fatalf("GetFS failed: %v", err)
	}

	entries, err := packFS.ReadDir("empty")
	if err != nil {
		t.Fatalf("ReadDir failed: %v", err)
	}

	if len(entries) != 1 {
		t.Errorf("Expected 1 entry, got %d", len(entries))
	}
}

func TestDeepNestedDirectories(t *testing.T) {
	fsys := fstest.MapFS{
		"a/b/c/d/e/file.txt": &fstest.MapFile{Data: []byte("deep"), Mode: 0644},
	}

	var buf bytes.Buffer
	writer := NewWriter()
	if err := writer.Pack(Metadata{}, nil, fsys, NewID("test", "deep"), nil, &buf); err != nil {
		t.Fatalf("Pack failed: %v", err)
	}

	reader, err := NewReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("NewReader failed: %v", err)
	}

	packFS, err := reader.GetFS(NewID("test", "deep"))
	if err != nil {
		t.Fatalf("GetFS failed: %v", err)
	}

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

func TestRealFilesystem(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "wapp-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	if err := os.WriteFile(filepath.Join(tmpDir, "test.txt"), []byte("real file"), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(tmpDir, "subdir"), 0755); err != nil {
		t.Fatalf("Failed to create subdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "subdir", "nested.txt"), []byte("nested"), 0644); err != nil {
		t.Fatalf("Failed to write nested file: %v", err)
	}

	fsys := os.DirFS(tmpDir)

	var buf bytes.Buffer
	writer := NewWriter()
	if err := writer.Pack(Metadata{}, nil, fsys, NewID("test", "real"), nil, &buf); err != nil {
		t.Fatalf("Pack failed: %v", err)
	}

	reader, err := NewReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("NewReader failed: %v", err)
	}

	packFS, err := reader.GetFS(NewID("test", "real"))
	if err != nil {
		t.Fatalf("GetFS failed: %v", err)
	}

	file, err := packFS.Open("test.txt")
	if err != nil {
		t.Fatalf("Open test.txt failed: %v", err)
	}
	data, _ := io.ReadAll(file)
	file.Close()

	if string(data) != "real file" {
		t.Errorf("Content = %q, want 'real file'", data)
	}

	file, err = packFS.Open("subdir/nested.txt")
	if err != nil {
		t.Fatalf("Open nested.txt failed: %v", err)
	}
	data, _ = io.ReadAll(file)
	file.Close()

	if string(data) != "nested" {
		t.Errorf("Nested content = %q, want 'nested'", data)
	}
}
