package wapp

import (
	"bytes"
	"io"
	"path/filepath"
	"testing"
	"testing/fstest"
)

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
	writer := NewWriter(WithProgressCallback(func(_ ID, current, total int) {
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
