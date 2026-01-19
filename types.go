package wapp

import "io/fs"

// Metadata is a string-keyed map for pack/resource metadata.
type Metadata map[string]any

// ID represents a namespaced identifier for resources and entries.
type ID struct {
	Namespace string `json:"ns" msgpack:"ns"`
	Name      string `json:"name" msgpack:"name"`
}

// NewID creates a new ID with namespace and name.
func NewID(ns, name string) ID {
	return ID{Namespace: ns, Name: name}
}

// String returns the string representation of ID.
func (id ID) String() string {
	if id.Namespace == "" {
		return id.Name
	}
	return id.Namespace + ":" + id.Name
}

// Equal checks if two IDs are equal.
func (id ID) Equal(other ID) bool {
	return id.Namespace == other.Namespace && id.Name == other.Name
}

// IsZero returns true if ID is empty.
func (id ID) IsZero() bool {
	return id.Namespace == "" && id.Name == ""
}

// Entry represents a registry entry stored in the pack.
type Entry struct {
	ID   ID       `json:"ID" msgpack:"ID"`
	Kind string   `json:"Kind" msgpack:"Kind"`
	Meta Metadata `json:"Meta,omitempty" msgpack:"Meta,omitempty"`
	Data any      `json:"Data,omitempty" msgpack:"Data,omitempty"`
}

// ResourceInfo provides summary information about a resource.
type ResourceInfo struct {
	ID        ID
	Type      string // ResourceTypeTree
	Meta      Metadata
	Hash      string
	Size      uint64
	FileCount uint32
}

// ResourceSpec specifies a filesystem resource to pack.
type ResourceSpec struct {
	ID   ID
	Meta Metadata
	FS   fs.FS // Must be non-nil.
}
