package wapp

import (
	"errors"
	"fmt"
)

// Sentinel errors for errors.Is checks.
var (
	ErrDataCorrupted         = errors.New("data corrupted")
	ErrNegativePosition      = errors.New("negative position")
	ErrIsDirectory           = errors.New("is a directory")
	ErrInvalidMagic          = errors.New("invalid magic")
	ErrUnsupportedVersion    = errors.New("unsupported version")
	ErrResourceNotFound      = errors.New("resource not found")
	ErrFrameNotFound         = errors.New("frame not found")
	ErrInvalidWhence         = errors.New("invalid whence")
	ErrDataSizeExceeded      = errors.New("data size exceeded")
	ErrResourceNotTree       = errors.New("resource not a tree")
	ErrUnknownResourceType   = errors.New("unknown resource type")
	ErrInsufficientFrames    = errors.New("insufficient frames")
	ErrResourceCountExceeded = errors.New("resource count exceeded")
	ErrInvalidResourceFS     = errors.New("invalid resource filesystem")
	ErrInvalidTOC            = errors.New("invalid toc")
	ErrFrameOutOfBounds      = errors.New("frame out of bounds")
	ErrFrameHashMismatch     = errors.New("frame hash mismatch")
)

// ReadError wraps errors during read operations.
type ReadError struct {
	Op   string
	Path string
	Err  error
}

func (e *ReadError) Error() string {
	if e.Path != "" {
		return fmt.Sprintf("wapp: %s %s: %v", e.Op, e.Path, e.Err)
	}
	return fmt.Sprintf("wapp: %s: %v", e.Op, e.Err)
}

func (e *ReadError) Unwrap() error { return e.Err }

// WriteError wraps errors during write operations.
type WriteError struct {
	Op   string
	Path string
	Err  error
}

func (e *WriteError) Error() string {
	if e.Path != "" {
		return fmt.Sprintf("wapp: %s %s: %v", e.Op, e.Path, e.Err)
	}
	return fmt.Sprintf("wapp: %s: %v", e.Op, e.Err)
}

func (e *WriteError) Unwrap() error { return e.Err }

// FormatError wraps format validation errors.
type FormatError struct {
	Op     string
	Detail string
	Err    error
}

func (e *FormatError) Error() string {
	if e.Detail != "" {
		return fmt.Sprintf("wapp: %s: %s", e.Op, e.Detail)
	}
	if e.Err != nil {
		return fmt.Sprintf("wapp: %s: %v", e.Op, e.Err)
	}
	return fmt.Sprintf("wapp: %s", e.Op)
}

func (e *FormatError) Unwrap() error { return e.Err }

// Reader error constructors.

func errReadHeader(cause error) error {
	return &ReadError{Op: "read header", Err: cause}
}

func errInvalidMagic(magic string) error {
	return &FormatError{Op: "validate", Detail: fmt.Sprintf("invalid magic %q", magic), Err: ErrInvalidMagic}
}

func errUnsupportedVersion(version byte) error {
	return &FormatError{Op: "validate", Detail: fmt.Sprintf("unsupported version %d", version), Err: ErrUnsupportedVersion}
}

func errDataSizeExceedsMax(dataSize, maxSize uint64) error {
	return &FormatError{Op: "validate", Detail: fmt.Sprintf("data size %d exceeds max %d", dataSize, maxSize), Err: ErrDataSizeExceeded}
}

func errReadData(cause error) error {
	return &ReadError{Op: "read data", Err: cause}
}

func errSeekToFooter(cause error) error {
	return &ReadError{Op: "seek footer", Err: cause}
}

func errReadFooter(cause error) error {
	return &ReadError{Op: "read footer", Err: cause}
}

func errReadTOC(cause error) error {
	return &ReadError{Op: "read TOC", Err: cause}
}

func errDecompressTOC(cause error) error {
	return &ReadError{Op: "decompress TOC", Err: cause}
}

func errDecodeTOC(cause error) error {
	return &ReadError{Op: "decode TOC", Err: cause}
}

func errReadMetadataFrame(cause error) error {
	return &ReadError{Op: "read metadata", Err: cause}
}

func errDecodeMetadata(cause error) error {
	return &ReadError{Op: "decode metadata", Err: cause}
}

func errReadEntriesFrame(cause error) error {
	return &ReadError{Op: "read entries", Err: cause}
}

func errDecodeEntries(cause error) error {
	return &ReadError{Op: "decode entries", Err: cause}
}

func errResourceNotTree(id string) error {
	return &ReadError{Op: "get tree", Path: id, Err: ErrResourceNotTree}
}

func errResourceNotFound(id string) error {
	return &ReadError{Op: "find resource", Path: id, Err: ErrResourceNotFound}
}

func errReadResourceFrame(cause error) error {
	return &ReadError{Op: "read resource", Err: cause}
}

func errDecodeTreeResource(cause error) error {
	return &ReadError{Op: "decode tree", Err: cause}
}

func errUnknownResourceType(resourceType string) error {
	return &FormatError{Op: "decode resource", Detail: fmt.Sprintf("unknown type %s", resourceType), Err: ErrUnknownResourceType}
}

func errReadFrame(cause error) error {
	return &ReadError{Op: "read frame", Err: cause}
}

func errReadFrameData(cause error) error {
	return &ReadError{Op: "read frame data", Err: cause}
}

func errFrameNotFound(frameIdx int) error {
	return &ReadError{Op: "find frame", Path: fmt.Sprintf("%d", frameIdx), Err: ErrFrameNotFound}
}

func errInvalidTOC(detail string) error {
	return &FormatError{Op: "validate toc", Detail: detail, Err: ErrInvalidTOC}
}

func errFrameOutOfBounds(detail string) error {
	return &FormatError{Op: "validate frame", Detail: detail, Err: ErrFrameOutOfBounds}
}

func errFrameHashMismatch(detail string) error {
	return &FormatError{Op: "verify frame hash", Detail: detail, Err: ErrFrameHashMismatch}
}

func errResetZstdDecoder(cause error) error {
	return &ReadError{Op: "reset zstd", Err: cause}
}

// Writer error constructors.

func errWriteHeader(cause error) error {
	return &WriteError{Op: "write header", Err: cause}
}

func errWriteFooter(cause error) error {
	return &WriteError{Op: "write footer", Err: cause}
}

func errCreateMetadataFrame(cause error) error {
	return &WriteError{Op: "create metadata frame", Err: cause}
}

func errCreateEntriesFrame(cause error) error {
	return &WriteError{Op: "create entries frame", Err: cause}
}

func errProcessFilesystem(cause error) error {
	return &WriteError{Op: "process filesystem", Err: cause}
}

func errCreateResourceFrame(resourceID string, cause error) error {
	return &WriteError{Op: "create resource frame", Path: resourceID, Err: cause}
}

func errProcessResourceFilesystem(resourceID string, cause error) error {
	return &WriteError{Op: "process filesystem", Path: resourceID, Err: cause}
}

func errStatFile(filePath string, cause error) error {
	return &WriteError{Op: "stat", Path: filePath, Err: cause}
}

func errOpenFile(filePath string, cause error) error {
	return &WriteError{Op: "open", Path: filePath, Err: cause}
}

func errReadFile(filePath string, cause error) error {
	return &WriteError{Op: "read", Path: filePath, Err: cause}
}

func errCompressFile(filePath string, cause error) error {
	return &WriteError{Op: "compress", Path: filePath, Err: cause}
}

func errMsgpackEncode(cause error) error {
	return &WriteError{Op: "msgpack encode", Err: cause}
}

func errZstdCompress(cause error) error {
	return &WriteError{Op: "zstd compress", Err: cause}
}

func errInsufficientFrames(got, need int) error {
	return &FormatError{Op: "validate frames", Detail: fmt.Sprintf("got %d, need %d", got, need), Err: ErrInsufficientFrames}
}

func errWriteFrame(frameIndex int, cause error) error {
	return &WriteError{Op: "write frame", Path: fmt.Sprintf("%d", frameIndex), Err: cause}
}

func errCreateTOCFrame(cause error) error {
	return &WriteError{Op: "create TOC frame", Err: cause}
}

func errWriteTOC(cause error) error {
	return &WriteError{Op: "write TOC", Err: cause}
}

func errInvalidResourceCount(count int) error {
	return &FormatError{Op: "validate resources", Detail: fmt.Sprintf("count %d exceeds limit", count), Err: ErrResourceCountExceeded}
}

func errInvalidResourceFS(resourceID string) error {
	return &WriteError{Op: "process filesystem", Path: resourceID, Err: ErrInvalidResourceFS}
}

// PackFS error constructors.

func errDecompress(cause error) error {
	return &ReadError{Op: "decompress", Err: cause}
}

func errInvalidWhence(whence int) error {
	return &FormatError{Op: "seek", Detail: fmt.Sprintf("invalid whence %d", whence), Err: ErrInvalidWhence}
}
