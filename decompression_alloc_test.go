package wapp

import (
	"bytes"
	"io"
	"sync"
	"testing"

	"github.com/klauspost/compress/zstd"
)

var decompressedSink []byte

func TestDecompressionDoesNotGrowOutputRepeatedly(t *testing.T) {
	encoder, err := zstd.NewWriter(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer encoder.Close()
	input := bytes.Repeat([]byte("embedded-resource-content"), 50000)
	compressed := encoder.EncodeAll(input, nil)
	decoder, err := zstd.NewReader(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer decoder.Close()
	direct := testing.AllocsPerRun(10, func() {
		decompressedSink, err = decoder.DecodeAll(compressed, nil)
		if err != nil {
			t.Fatal(err)
		}
	})
	actual := testing.AllocsPerRun(10, func() {
		decompressedSink, err = decompressZstd(compressed)
		if err != nil {
			t.Fatal(err)
		}
	})
	if !bytes.Equal(decompressedSink, input) {
		t.Fatal("decompressed content changed")
	}
	if actual > direct+2 {
		t.Fatalf("decompression allocated %.0f objects versus direct decoder's %.0f", actual, direct)
	}
}

func TestDecompressionStreamParity(t *testing.T) {
	encoder, err := zstd.NewWriter(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer encoder.Close()
	first := encoder.EncodeAll([]byte("first frame"), nil)
	second := encoder.EncodeAll([]byte("second frame"), nil)
	badCRC := append([]byte(nil), first...)
	badCRC[len(badCRC)-1] ^= 1
	skippable := []byte{0x50, 0x2a, 0x4d, 0x18, 3, 0, 0, 0, 's', 'k', 'p'}
	for _, data := range [][]byte{
		nil, {}, first, append(append([]byte(nil), first...), second...),
		first[:len(first)-1], first[:4], []byte("not zstd"),
		badCRC, append(append([]byte(nil), first...), []byte("garbage")...),
		append(append([]byte(nil), skippable...), first...), skippable,
	} {
		decoder, err := zstd.NewReader(bytes.NewReader(data))
		var expected []byte
		if err == nil {
			expected, err = io.ReadAll(decoder)
			decoder.Close()
		}
		actual, actualErr := decompressZstd(data)
		if (actualErr == nil) != (err == nil) {
			t.Fatalf("input %x: stream error=%v direct error=%v", data, err, actualErr)
		}
		if err == nil && !bytes.Equal(actual, expected) {
			t.Fatalf("successful decode changed bytes: %q versus %q", actual, expected)
		}
	}
}

func TestDecompressionPoolOwnershipAndRecovery(t *testing.T) {
	encoder, err := zstd.NewWriter(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer encoder.Close()
	input := bytes.Repeat([]byte("independent decoded contents"), 10000)
	compressed := encoder.EncodeAll(input, nil)
	first, err := decompressZstd(compressed)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 10 {
				if _, err := decompressZstd([]byte("corrupt")); err == nil {
					t.Error("corruption was accepted")
				}
				got, err := decompressZstd(compressed)
				if err != nil || !bytes.Equal(got, input) {
					t.Errorf("pool recovery failed: %v", err)
					return
				}
				got[0] ^= 1
			}
		}()
	}
	wg.Wait()
	if !bytes.Equal(first, input) {
		t.Fatal("a later decode mutated a previously returned buffer")
	}
}

func BenchmarkDecompressionResource(b *testing.B) {
	encoder, err := zstd.NewWriter(nil)
	if err != nil {
		b.Fatal(err)
	}
	defer encoder.Close()
	compressed := encoder.EncodeAll(bytes.Repeat([]byte("embedded-resource-content"), 50000), nil)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		decompressedSink, err = decompressZstd(compressed)
		if err != nil {
			b.Fatal(err)
		}
	}
}
