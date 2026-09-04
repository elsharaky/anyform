package anyform

import (
	"errors"
	"fmt"
	"strings"
)

// Sentinel errors returned by anyform operations.
var (
	// ErrNotStruct is returned when Marshal or Unmarshal receives a non-struct type.
	ErrNotStruct = errors.New("anyform: expected struct type")

	// ErrNilPointer is passed a nil pointer to Marshal or Unmarshal.
	ErrNilPointer = errors.New("anyform: nil pointer")

	// ErrMissingRequired is returned when a required field is absent during Unmarshal.
	ErrMissingRequired = errors.New("anyform: missing required field")

	// ErrFileNotSupported is returned when a File field is encountered during
	// URL-value marshalling. Use MarshalMultipart instead.
	ErrFileNotSupported = errors.New("anyform: File fields require multipart encoding; use MarshalMultipart")

	// ErrMaxDepthExceeded is returned when nested struct depth exceeds the configured limit.
	ErrMaxDepthExceeded = errors.New("anyform: max nesting depth exceeded")

	// ErrBodyTooLarge is returned when a body exceeds the limit set by WithMaxBodySize.
	ErrBodyTooLarge = errors.New("anyform: body exceeds maximum size")

	// ErrFileTooLarge is returned when a file part exceeds the limit set by WithMaxFileSize.
	ErrFileTooLarge = errors.New("anyform: file exceeds maximum size")
)

// EncodingError wraps a field path and underlying cause during marshalling.
type EncodingError struct {
	FieldPath string
	Err       error
}

func (e *EncodingError) Error() string {
	if e.FieldPath != "" {
		return fmt.Sprintf("anyform: encoding field %s: %v", e.FieldPath, e.Err)
	}
	return fmt.Sprintf("anyform: encoding: %v", e.Err)
}

func (e *EncodingError) Unwrap() error { return e.Err }

// DecodingError wraps a field path, key, and underlying cause during unmarshalling.
type DecodingError struct {
	FieldPath string
	Key       string
	Err       error
}

func (e *DecodingError) Error() string {
	var b strings.Builder
	b.WriteString("anyform: decoding")
	if e.FieldPath != "" {
		fmt.Fprintf(&b, " field %s", e.FieldPath)
	}
	if e.Key != "" {
		fmt.Fprintf(&b, " (key %q)", e.Key)
	}
	fmt.Fprintf(&b, ": %v", e.Err)
	return b.String()
}

func (e *DecodingError) Unwrap() error { return e.Err }
