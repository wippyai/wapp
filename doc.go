// Package wapp provides the WAPP (Wippy Application Pack) binary archive format.
//
// WAPP is designed for packaging filesystem trees, registry entries, and metadata
// into a single binary file. It supports lazy loading, per-file compression, and
// integrity verification.
//
// # Format Structure
//
// A WAPP file consists of:
//   - Header (268 bytes): Magic, version, data offset/size, SHA-256 hash
//   - Data frames: Compressed file content in 10MB frames
//   - TOC: Msgpack-encoded table of contents (zstd compressed)
//   - Footer (16 bytes): TOC offset and size
//
// # Contents
//
// A WAPP file can contain:
//   - Metadata: Pack-level key-value metadata
//   - Registry Entries: Typed records with ID, Kind, Meta, and Data fields
//   - Resources: Filesystem trees with per-file compression
//
// # Writing
//
// Use [Writer] to create WAPP files:
//
//	writer := wapp.NewWriter()
//	err := writer.Pack(metadata, entries, fsys, resourceID, resourceMeta, output)
//
// # Reading
//
// Use [Reader] to read WAPP files:
//
//	reader, err := wapp.NewReader(file)
//	entries, err := reader.GetEntries()
//	fsys, err := reader.GetFS(resourceID)
//
// # Features
//
//   - Per-file zstd compression (skips already-compressed formats)
//   - Lazy loading with footer-first reading
//   - Multiple filesystem tree resources per pack
//   - Registry entries for structured data storage
//   - SHA-256 integrity verification
//   - O(1) file and resource lookups
//   - Concurrent-safe reads with decompression cache
//   - fs.FS interface compatibility
package wapp
