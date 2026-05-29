package wapp

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"io"
	mrand "math/rand"
	"testing"
	"testing/fstest"
)

// randomBytes returns n incompressible bytes.
func randomBytes(t *testing.T, n int) []byte {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	return b
}

// TestLargeChunkedFilesRoundTrip packs several files whose post-compression
// payload exceeds ChunkSize so they split into many chunks across multiple
// data frames, then re-opens the pack and verifies every file is byte-identical.
//
// The big file is named so it decodes (canonical key order) after other chunked
// files, mirroring a real module with many embedded assets. This exercises the
// msgpack in-place decode reuse path for map values and slice elements.
func TestLargeChunkedFilesRoundTrip(t *testing.T) {
	// .js is NOT skip-compress, so the writer zstd-compresses it; random bytes are
	// incompressible so finalData stays > ChunkSize and chunking is guaranteed.
	//
	// The trigger for the in-place-reuse bug: two chunked files ADJACENT in canonical
	// key order, where the later one has FEWER chunks. msgpack reuses one temp
	// FileEntry across map values and copies it shallowly into the map, so the map
	// entry and temp share the chunk-slice backing array; decoding the shorter later
	// file overwrites the first N elements of that shared array in place, corrupting
	// the earlier file's chunk table. (A nil-chunk small file between them would reset
	// the temp and hide the bug, so the two chunked files must be adjacent.)
	files := map[string][]byte{
		"assets/000_small.txt": []byte("hello world"),
		"assets/bundle_a.js":   randomBytes(t, 18*1024*1024+4567), // sorts first; ~18 MiB -> 19 chunks
		"assets/bundle_b.js":   randomBytes(t, 3*1024*1024+777),   // adjacent, fewer chunks (~4)
		"assets/zzz_small.txt": []byte("another small file"),
	}

	fsys := fstest.MapFS{}
	for name, data := range files {
		fsys[name] = &fstest.MapFile{Data: data, Mode: 0o644}
	}

	var buf bytes.Buffer
	w := NewWriter()
	id := NewID("test", "chunked")
	if err := w.Pack(Metadata{}, nil, fsys, id, nil, &buf); err != nil {
		t.Fatalf("Pack: %v", err)
	}

	r, err := NewReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}

	pfs, err := r.GetFS(id)
	if err != nil {
		t.Fatalf("GetFS (validates chunk table): %v", err)
	}

	// The reported failure was at chunk index 14, so guard that the big file
	// genuinely splits into more than 14 chunks across multiple frames.
	if pf, ok := pfs.(*packFS); ok {
		if got := len(pf.tree.Files["assets/bundle_a.js"].Location.Chunks); got <= 14 {
			t.Fatalf("expected bundle_a.js to have >14 chunks, got %d", got)
		}
	} else {
		t.Fatalf("GetFS returned %T, want *packFS", pfs)
	}

	for name, want := range files {
		f, err := pfs.Open(name)
		if err != nil {
			t.Fatalf("Open %q: %v", name, err)
		}
		got, err := io.ReadAll(f)
		_ = f.Close()
		if err != nil {
			t.Fatalf("read %q: %v", name, err)
		}
		if len(got) != len(want) {
			t.Fatalf("file %q length = %d, want %d", name, len(got), len(want))
		}
		if sha256.Sum256(got) != sha256.Sum256(want) {
			t.Fatalf("file %q content mismatch", name)
		}
	}
}

// TestChunkedRoundTripFuzz packs many randomized (but seed-deterministic) module
// shapes and verifies every file reads back byte-identical. Sizes are biased to
// chunk (1 MiB) and frame (10 MiB) boundaries, and names are randomized so the
// canonical decode order — and thus the adjacency of chunked files that triggered
// the backing-array reuse corruption — varies across iterations.
func TestChunkedRoundTripFuzz(t *testing.T) {
	const chunk = 1 << 20
	pickSize := func(rng *mrand.Rand) int {
		switch rng.Intn(8) {
		case 0:
			return rng.Intn(4096) // tiny / below ChunkSize
		case 1:
			return chunk // exact boundary
		case 2:
			return chunk + rng.Intn(3) - 1 // boundary +/-1
		case 3:
			return (2 + rng.Intn(4)) * chunk // multi-chunk
		default:
			return rng.Intn(5 * chunk) // mixed
		}
	}

	for seed := int64(0); seed < 40; seed++ {
		seed := seed
		t.Run(fmt.Sprintf("seed-%d", seed), func(t *testing.T) {
			rng := mrand.New(mrand.NewSource(seed))
			fsys := fstest.MapFS{}
			want := map[string][32]byte{}
			n := 2 + rng.Intn(8)
			for i := 0; i < n; i++ {
				name := fmt.Sprintf("assets/%08x.js", rng.Uint32())
				data := make([]byte, pickSize(rng))
				_, _ = rng.Read(data) // incompressible -> stays chunked
				fsys[name] = &fstest.MapFile{Data: data, Mode: 0o644}
				want[name] = sha256.Sum256(data)
			}

			var buf bytes.Buffer
			id := NewID("fuzz", "public_files")
			if err := NewWriter().Pack(Metadata{}, nil, fsys, id, nil, &buf); err != nil {
				t.Fatalf("Pack: %v", err)
			}
			r, err := NewReader(bytes.NewReader(buf.Bytes()))
			if err != nil {
				t.Fatalf("NewReader: %v", err)
			}
			pfs, err := r.GetFS(id)
			if err != nil {
				t.Fatalf("GetFS (validate toc): %v", err)
			}
			for name, sum := range want {
				f, err := pfs.Open(name)
				if err != nil {
					t.Fatalf("Open %q: %v", name, err)
				}
				got, err := io.ReadAll(f)
				_ = f.Close()
				if err != nil {
					t.Fatalf("read %q: %v", name, err)
				}
				if sha256.Sum256(got) != sum {
					t.Fatalf("file %q content mismatch (%d bytes)", name, len(got))
				}
			}
		})
	}
}
