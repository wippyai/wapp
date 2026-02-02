package wapp

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

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
		if !strings.Contains(err.Error(), "test") {
			t.Error("Error should contain operation")
		}
	})

	t.Run("FormatErrorNoErr", func(t *testing.T) {
		err := &FormatError{Op: "test"}
		if !strings.Contains(err.Error(), "test") {
			t.Error("Error should contain operation")
		}
	})
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

		if readErr.Path != "nonexistent:resource" {
			t.Errorf("Path = %q, want 'nonexistent:resource'", readErr.Path)
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
