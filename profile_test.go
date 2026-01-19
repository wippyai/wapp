package wapp

import (
	"bytes"
	"io"
	"testing"
	"testing/fstest"
)

func BenchmarkNewReader(b *testing.B) {
	fsys := fstest.MapFS{
		"file1.txt": &fstest.MapFile{Data: []byte("content1"), Mode: 0644},
		"file2.txt": &fstest.MapFile{Data: []byte("content2"), Mode: 0644},
	}

	var buf bytes.Buffer
	writer := NewWriter()
	_ = writer.Pack(Metadata{"name": "test"}, []Entry{{ID: NewID("t", "e"), Kind: "test"}}, fsys, NewID("test", "res"), nil, &buf)
	data := buf.Bytes()

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = NewReader(bytes.NewReader(data))
	}
}

func BenchmarkGetMetadata(b *testing.B) {
	fsys := fstest.MapFS{
		"file.txt": &fstest.MapFile{Data: []byte("content"), Mode: 0644},
	}

	var buf bytes.Buffer
	writer := NewWriter()
	_ = writer.Pack(Metadata{"name": "test", "version": "1.0.0"}, nil, fsys, NewID("test", "res"), nil, &buf)
	data := buf.Bytes()

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		reader, _ := NewReader(bytes.NewReader(data))
		_, _ = reader.GetMetadata()
	}
}

func BenchmarkGetEntries(b *testing.B) {
	entries := make([]Entry, 100)
	for i := range entries {
		entries[i] = Entry{
			ID:   NewID("ns", "entry"),
			Kind: "test.kind",
			Meta: Metadata{"key": "value"},
		}
	}

	var buf bytes.Buffer
	writer := NewWriter()
	_ = writer.PackEntries(Metadata{}, entries, &buf)
	data := buf.Bytes()

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		reader, _ := NewReader(bytes.NewReader(data))
		_, _ = reader.GetEntries()
	}
}

func BenchmarkGetFS(b *testing.B) {
	fsys := fstest.MapFS{
		"a.txt": &fstest.MapFile{Data: []byte("a"), Mode: 0644},
		"b.txt": &fstest.MapFile{Data: []byte("b"), Mode: 0644},
	}

	var buf bytes.Buffer
	writer := NewWriter()
	_ = writer.Pack(Metadata{}, nil, fsys, NewID("test", "fs"), nil, &buf)
	data := buf.Bytes()

	reader, _ := NewReader(bytes.NewReader(data))

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = reader.GetFS(NewID("test", "fs"))
	}
}

func BenchmarkReadSmallFile(b *testing.B) {
	content := bytes.Repeat([]byte("x"), 1024)
	fsys := fstest.MapFS{
		"file.txt": &fstest.MapFile{Data: content, Mode: 0644},
	}

	var buf bytes.Buffer
	writer := NewWriter()
	_ = writer.Pack(Metadata{}, nil, fsys, NewID("test", "read"), nil, &buf)
	data := buf.Bytes()

	reader, _ := NewReader(bytes.NewReader(data))
	packFS, _ := reader.GetFS(NewID("test", "read"))

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		file, _ := packFS.Open("file.txt")
		_, _ = io.ReadAll(file)
		_ = file.Close()
	}
}

func BenchmarkReadLargeFile(b *testing.B) {
	content := bytes.Repeat([]byte("x"), 512*1024)
	fsys := fstest.MapFS{
		"large.bin": &fstest.MapFile{Data: content, Mode: 0644},
	}

	var buf bytes.Buffer
	writer := NewWriter()
	_ = writer.Pack(Metadata{}, nil, fsys, NewID("test", "large"), nil, &buf)
	data := buf.Bytes()

	reader, _ := NewReader(bytes.NewReader(data))
	packFS, _ := reader.GetFS(NewID("test", "large"))

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		file, _ := packFS.Open("large.bin")
		_, _ = io.ReadAll(file)
		_ = file.Close()
	}
}

func BenchmarkOpenManyFiles(b *testing.B) {
	fsys := make(fstest.MapFS)
	for i := 0; i < 50; i++ {
		name := string(rune('a'+i/26)) + string(rune('a'+i%26)) + ".txt"
		fsys[name] = &fstest.MapFile{Data: []byte("content"), Mode: 0644}
	}

	var buf bytes.Buffer
	writer := NewWriter()
	_ = writer.Pack(Metadata{}, nil, fsys, NewID("test", "many"), nil, &buf)
	data := buf.Bytes()

	reader, _ := NewReader(bytes.NewReader(data))
	packFS, _ := reader.GetFS(NewID("test", "many"))

	names := make([]string, 0, len(fsys))
	for name := range fsys {
		names = append(names, name)
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		for _, name := range names {
			file, _ := packFS.Open(name)
			_ = file.Close()
		}
	}
}

func BenchmarkRepeatedFileRead(b *testing.B) {
	content := bytes.Repeat([]byte("x"), 4096)
	fsys := fstest.MapFS{
		"file.txt": &fstest.MapFile{Data: content, Mode: 0644},
	}

	var buf bytes.Buffer
	writer := NewWriter()
	_ = writer.Pack(Metadata{}, nil, fsys, NewID("test", "repeat"), nil, &buf)
	data := buf.Bytes()

	reader, _ := NewReader(bytes.NewReader(data))
	packFS, _ := reader.GetFS(NewID("test", "repeat"))

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		file, _ := packFS.Open("file.txt")
		_, _ = io.ReadAll(file)
		_ = file.Close()
	}
}

func BenchmarkSequentialFileReads(b *testing.B) {
	fsys := make(fstest.MapFS)
	for i := 0; i < 10; i++ {
		name := string(rune('a'+i)) + ".txt"
		fsys[name] = &fstest.MapFile{Data: bytes.Repeat([]byte("x"), 1024), Mode: 0644}
	}

	var buf bytes.Buffer
	writer := NewWriter()
	_ = writer.Pack(Metadata{}, nil, fsys, NewID("test", "seq"), nil, &buf)
	data := buf.Bytes()

	reader, _ := NewReader(bytes.NewReader(data))
	packFS, _ := reader.GetFS(NewID("test", "seq"))

	names := make([]string, 0, 10)
	for i := 0; i < 10; i++ {
		names = append(names, string(rune('a'+i))+".txt")
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		for _, name := range names {
			file, _ := packFS.Open(name)
			_, _ = io.ReadAll(file)
			_ = file.Close()
		}
	}
}

func BenchmarkFullBootSimulation(b *testing.B) {
	fsys := make(fstest.MapFS)
	for i := 0; i < 20; i++ {
		name := string(rune('a'+i%26)) + ".lua"
		fsys[name] = &fstest.MapFile{Data: bytes.Repeat([]byte("-- lua code\n"), 100), Mode: 0644}
	}

	entries := make([]Entry, 50)
	for i := range entries {
		entries[i] = Entry{
			ID:   NewID("app", "entry"),
			Kind: "service.handler",
			Meta: Metadata{"priority": i},
		}
	}

	var buf bytes.Buffer
	writer := NewWriter()
	_ = writer.Pack(Metadata{"name": "myapp", "version": "1.0.0"}, entries, fsys, NewID("app", "scripts"), nil, &buf)
	data := buf.Bytes()

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		reader, _ := NewReader(bytes.NewReader(data))
		_, _ = reader.GetMetadata()
		_, _ = reader.GetEntries()
		packFS, _ := reader.GetFS(NewID("app", "scripts"))

		for name := range fsys {
			file, _ := packFS.Open(name)
			_, _ = io.ReadAll(file)
			_ = file.Close()
		}
	}
}
