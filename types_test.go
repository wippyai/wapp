package wapp

import (
	"testing"
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
			{NewID("ns", "name"), "ns:name"},
			{NewID("", "name"), "name"},
			{NewID("org", "pkg"), "org:pkg"},
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
		id4 := NewID("other", "name")

		if !id1.Equal(id2) {
			t.Error("Equal IDs should be equal")
		}
		if id1.Equal(id3) {
			t.Error("Different names should not be equal")
		}
		if id1.Equal(id4) {
			t.Error("Different namespaces should not be equal")
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

	t.Run("EmptyNamespace", func(t *testing.T) {
		id := NewID("", "name")
		if id.String() != "name" {
			t.Errorf("String() = %q, want %q", id.String(), "name")
		}
	})
}

func TestEntryWithData(t *testing.T) {
	entry := Entry{
		ID:   NewID("test", "entry"),
		Kind: "data.entry",
		Meta: Metadata{"key": "value"},
		Data: []byte("binary data"),
	}

	if entry.ID.IsZero() {
		t.Error("Entry ID should not be zero")
	}
	if entry.Kind != "data.entry" {
		t.Errorf("Entry Kind = %q, want %q", entry.Kind, "data.entry")
	}
}
