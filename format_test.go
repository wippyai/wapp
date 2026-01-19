package wapp

import (
	"bytes"
	"testing"
)

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

	t.Run("ShortHeaderData", func(t *testing.T) {
		data := []byte("WAPP")
		_, err := NewReader(bytes.NewReader(data))
		if err == nil {
			t.Error("Expected error for short data")
		}
	})
}

func TestCompressionSkip(t *testing.T) {
	skipExts := []string{".png", ".jpg", ".jpeg", ".gif", ".webp", ".ico",
		".woff", ".woff2", ".ttf", ".otf", ".mp4", ".webm", ".mp3",
		".ogg", ".wav", ".avi", ".mov", ".gz", ".zip", ".br", ".zst",
		".7z", ".rar", ".bz2", ".xz"}

	for _, ext := range skipExts {
		if DefaultCompressionFunc("file" + ext) {
			t.Errorf("Should skip compression for %s", ext)
		}
	}

	compressExts := []string{".txt", ".js", ".css", ".html", ".json", ".lua"}
	for _, ext := range compressExts {
		if !DefaultCompressionFunc("file" + ext) {
			t.Errorf("Should compress %s", ext)
		}
	}
}
