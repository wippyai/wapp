# wapp

WAPP (Wippy Application Pack) is a binary archive format for packaging filesystem trees with metadata.

## Format Structure

```
Header (268 bytes) + Data Frames + Compressed TOC + Footer (16 bytes)
```

- **Header**: Magic number, version, data offset/size, SHA-256 hash
- **Data Frames**: Compressed file content in 10MB frames
- **TOC**: Msgpack-encoded table of contents (zstd compressed)
- **Footer**: TOC offset and size for footer-first reading

## Features

- Per-file zstd compression (skips already-compressed formats)
- Lazy loading with footer-first reading
- Multiple filesystem tree resources per pack
- SHA-256 integrity verification
- O(1) file and resource lookups
- Concurrent-safe reads with decompression cache
- fs.FS interface compatibility

## Installation

```bash
go get github.com/wippyai/wapp
```

## Usage

### Writing

```go
writer := wapp.NewWriter()

// Single resource
err := writer.Pack(
    wapp.Metadata{"version": "1.0"},
    []wapp.Entry{{ID: wapp.NewID("app", "config"), Kind: "config"}},
    os.DirFS("./myapp"),
    wapp.NewID("app", "files"),
    nil,
    outputFile,
)

// Multiple resources
err := writer.PackWithResources(
    metadata,
    entries,
    []wapp.ResourceSpec{
        {ID: wapp.NewID("app", "frontend"), FS: os.DirFS("./frontend")},
        {ID: wapp.NewID("app", "backend"), FS: os.DirFS("./backend")},
    },
    outputFile,
)

// Entries only (no filesystem)
err := writer.PackEntries(metadata, entries, outputFile)
```

### Reading

```go
reader, err := wapp.NewReader(file)

// Get metadata
meta, err := reader.GetMetadata()

// Get entries
entries, err := reader.GetEntries()

// List resources
resources := reader.ListResources()

// Get filesystem
fsys, err := reader.GetFS(wapp.NewID("app", "files"))

// Use standard fs operations
data, err := fs.ReadFile(fsys, "config.json")
entries, err := fs.ReadDir(fsys, "templates")
```

### Options

```go
// Custom compression decision
writer := wapp.NewWriter(
    wapp.WithCompressionFunc(func(path string) bool {
        return !strings.HasSuffix(path, ".gz")
    }),
    wapp.WithProgressCallback(func(id wapp.ID, current, total int) {
        fmt.Printf("%s: %d/%d\n", id, current, total)
    }),
)

// Custom decompression cache
reader, err := wapp.NewReaderWithOptions(file,
    wapp.WithDecompressionCacheLimit(128 << 20), // 128MB
)
```

## Dependencies

- `github.com/klauspost/compress/zstd` - zstd compression
- `github.com/hashicorp/go-msgpack/v2` - msgpack serialization
