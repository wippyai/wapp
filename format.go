package wapp

import (
	"bytes"
	"encoding/binary"
	"io"
	"reflect"
	"sync"

	"github.com/hashicorp/go-msgpack/v2/codec"
	"github.com/klauspost/compress/zstd"
)

const (
	// Magic header identifying WAPP files.
	Magic = "WIPPYPACK"

	// Version1 is the current format version.
	Version1 byte = 0x01

	// HeaderSize is the fixed header size in bytes.
	HeaderSize = 268

	// FooterSize is the fixed footer size in bytes.
	FooterSize = 16
)

// Header represents the WAPP file header.
type Header struct {
	Magic      [9]byte   // "WIPPYPACK"
	Version    byte      // 0x01
	Flags      uint16    // Reserved flags
	DataOffset uint64    // Offset to data section
	DataSize   uint64    // Total size of data section
	DataHash   [32]byte  // SHA-256 of data section
	Reserved   [208]byte // Reserved for future use
}

// Footer represents the WAPP file footer.
type Footer struct {
	TOCOffset uint64 // Offset to compressed TOC
	TOCSize   uint64 // Compressed TOC size
}

// ReadHeader reads and validates a WAPP header.
func ReadHeader(r io.Reader) (*Header, error) {
	h := &Header{}
	if err := binary.Read(r, binary.LittleEndian, h); err != nil {
		return nil, errReadHeader(err)
	}

	if string(h.Magic[:]) != Magic {
		return nil, errInvalidMagic(string(h.Magic[:]))
	}

	if h.Version != Version1 {
		return nil, errUnsupportedVersion(h.Version)
	}

	return h, nil
}

// WriteHeader writes a WAPP header.
func WriteHeader(w io.Writer, h *Header) error {
	copy(h.Magic[:], Magic)
	h.Version = Version1

	if err := binary.Write(w, binary.LittleEndian, h); err != nil {
		return errWriteHeader(err)
	}

	return nil
}

// ReadFooter reads a WAPP footer from a seeker.
func ReadFooter(r io.ReadSeeker) (*Footer, error) {
	if _, err := r.Seek(-FooterSize, io.SeekEnd); err != nil {
		return nil, errSeekToFooter(err)
	}

	f := &Footer{}
	if err := binary.Read(r, binary.LittleEndian, f); err != nil {
		return nil, errReadFooter(err)
	}

	return f, nil
}

// WriteFooter writes a WAPP footer.
func WriteFooter(w io.Writer, f *Footer) error {
	if err := binary.Write(w, binary.LittleEndian, f); err != nil {
		return errWriteFooter(err)
	}

	return nil
}

// TOC represents the table of contents.
type TOC struct {
	// Pack metadata frame.
	Metadata FrameInfo `json:"Metadata" msgpack:"metadata"`

	// Registry entries frame.
	Entries FrameInfo `json:"Entries" msgpack:"entries"`

	// Resource frames (filesystem trees).
	Resources []ResourceFrame `json:"Resources" msgpack:"resources"`

	// Data frames containing file content.
	DataFrames []FrameInfo `json:"DataFrames" msgpack:"data_frames"`
}

// FrameInfo describes a data frame location and integrity.
type FrameInfo struct {
	Offset           uint64 `json:"Offset" msgpack:"offset"`
	Size             uint64 `json:"Size" msgpack:"size"`
	UncompressedSize uint64 `json:"UncompressedSize" msgpack:"uncomp_size"`
	// Hash is the hex-encoded SHA-256 of the compressed frame bytes.
	Hash string `json:"Hash" msgpack:"hash"`
}

// ResourceFrame describes a resource in the pack.
type ResourceFrame struct {
	ID        ID        `json:"ID" msgpack:"id"`
	Type      string    `json:"Type" msgpack:"type"` // ResourceTypeTree
	Meta      Metadata  `json:"Meta" msgpack:"meta"`
	Frame     FrameInfo `json:"Frame" msgpack:"frame"`
	FileCount uint32    `json:"FileCount,omitempty" msgpack:"file_count,omitempty"`
	TotalSize uint64    `json:"TotalSize,omitempty" msgpack:"total_size,omitempty"`
}

// newMsgpackHandle creates a msgpack handle with standard configuration.
func newMsgpackHandle() *codec.MsgpackHandle {
	mh := &codec.MsgpackHandle{}
	mh.MapType = reflect.TypeOf(map[string]interface{}(nil))
	mh.SliceType = nil
	mh.RawToString = true
	mh.Canonical = true
	mh.StructToArray = false
	return mh
}

// zstdDecoderPool pools zstd decoders to reduce allocations.
var zstdDecoderPool = sync.Pool{
	New: func() interface{} {
		decoder, _ := zstd.NewReader(nil)
		return decoder
	},
}

// zstdEncoderPool pools zstd encoders to reduce allocations.
var zstdEncoderPool = sync.Pool{
	New: func() interface{} {
		encoder, _ := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedDefault))
		return encoder
	},
}

// decompressZstd decompresses zstd-compressed data using pooled decoder.
func decompressZstd(compressed []byte) ([]byte, error) {
	decoder := zstdDecoderPool.Get().(*zstd.Decoder)
	defer zstdDecoderPool.Put(decoder)

	err := decoder.Reset(bytes.NewReader(compressed))
	if err != nil {
		return nil, errResetZstdDecoder(err)
	}

	return io.ReadAll(decoder)
}

// compressZstd compresses data using pooled encoder.
func compressZstd(data []byte, buf *bytes.Buffer) error {
	encoder := zstdEncoderPool.Get().(*zstd.Encoder)
	defer zstdEncoderPool.Put(encoder)

	encoder.Reset(buf)
	if _, err := encoder.Write(data); err != nil {
		return err
	}
	return encoder.Close()
}

func compressZstdWithLevel(data []byte, buf *bytes.Buffer, level zstd.EncoderLevel) error {
	if level == zstd.SpeedDefault {
		return compressZstd(data, buf)
	}

	encoder, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(level))
	if err != nil {
		return err
	}

	encoder.Reset(buf)
	if _, err := encoder.Write(data); err != nil {
		_ = encoder.Close()
		return err
	}
	if err := encoder.Close(); err != nil {
		return err
	}
	return nil
}
