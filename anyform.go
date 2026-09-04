package anyform

import (
	"bytes"
	"errors"
	"mime"
	"mime/multipart"
	"net/url"
	"reflect"
)

// urlEncodedContentType is the Content-Type for URL-encoded bodies.
const urlEncodedContentType = "application/x-www-form-urlencoded"

// Marshal serializes v into a request body and returns the body bytes together
// with an appropriate Content-Type header value.
//
// The serialization format is chosen automatically:
//
//   - If v (recursively, including nested structs, slices, maps, and pointers)
//     contains fields of type File or []File, the body is built as
//     multipart/form-data and the returned Content-Type includes the boundary.
//   - Otherwise the body is application/x-www-form-urlencoded.
//
// Functional options configure this single call; no state is shared between
// calls, so repeated Marshal calls are safe from any goroutine.
//
// Example:
//
//	body, ct, err := anyform.Marshal(req{Name: "Alice"})
func Marshal(v any, opts ...Option) ([]byte, string, error) {
	cfg := newConfig(opts...)
	enc := &Encoder{cfg: cfg, resolver: newTagResolver(cfg.tagPriority...)}

	rv, err := addressableValue(v)
	if err != nil {
		return nil, "", &EncodingError{Err: err}
	}

	if scanForFiles(rv, make(map[reflect.Type]bool), 0) {
		return enc.MarshalMultipart(v)
	}

	vals, merr := enc.Marshal(v)
	if merr != nil {
		return nil, "", merr
	}
	return []byte(vals.Encode()), urlEncodedContentType, nil
}

// Unmarshal decodes request body bytes into the struct pointed to by v,
// detecting the format from the Content-Type header.
//
//   - If contentType indicates multipart/form-data, the body is parsed as
//     multipart data and File/[]File fields are populated from file parts.
//   - Otherwise the body is treated as application/x-www-form-urlencoded.
//
// v must be a non-nil pointer to a struct. Functional options configure this
// single call and are never shared between calls.
//
// Example:
//
//	err := anyform.Unmarshal(body, ct, &req)
func Unmarshal(body []byte, contentType string, v any, opts ...Option) error {
	cfg := newConfig(opts...)
	if cfg.maxBodySize > 0 && int64(len(body)) > cfg.maxBodySize {
		return &DecodingError{Err: ErrBodyTooLarge}
	}
	dec := &Decoder{cfg: cfg, resolver: newTagResolver(cfg.tagPriority...)}

	if isMultipartContentType(contentType) {
		return dec.unmarshalMultipartBody(body, contentType, v)
	}

	values, err := url.ParseQuery(string(body))
	if err != nil {
		return &DecodingError{Err: err}
	}
	return dec.Unmarshal(values, v)
}

// unmarshalMultipartBody parses a raw multipart body into v using the boundary
// encoded in the Content-Type header.
func (d *Decoder) unmarshalMultipartBody(body []byte, contentType string, v any) error {
	_, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return &DecodingError{Err: err}
	}
	boundary := params["boundary"]
	if boundary == "" {
		return &DecodingError{Err: errors.New("anyform: multipart Content-Type missing boundary")}
	}

	mr := multipart.NewReader(bytes.NewReader(body), boundary)
	mf, rerr := mr.ReadForm(32 << 20)
	if rerr != nil {
		return &DecodingError{Err: rerr}
	}
	defer func() { _ = mf.RemoveAll() }()

	return d.UnmarshalMultipartForm(mf, v)
}

// newConfig builds a config from functional options.
func newConfig(opts ...Option) *config {
	cfg := defaultConfig()
	for _, opt := range opts {
		if opt != nil {
			opt(cfg)
		}
	}
	return cfg
}

func isMultipartContentType(contentType string) bool {
	mt, _, err := mime.ParseMediaType(contentType)
	return err == nil && mt == "multipart/form-data"
}

// scanForFiles walks v (recursively) and reports whether it contains any
// File or []File field that would require multipart serialization.
// visited guards against self-referential types to avoid infinite recursion.
func scanForFiles(rv reflect.Value, visited map[reflect.Type]bool, depth int) bool {
	if !rv.IsValid() || depth > defaultMaxDepth {
		return false
	}

	switch rv.Kind() {
	case reflect.Pointer, reflect.Interface:
		if rv.IsNil() {
			return false
		}
		return scanForFiles(rv.Elem(), visited, depth+1)
	case reflect.Slice, reflect.Array:
		for i := range rv.Len() {
			if scanForFiles(rv.Index(i), visited, depth+1) {
				return true
			}
		}
		return false
	case reflect.Map:
		iter := rv.MapRange()
		for iter.Next() {
			if scanForFiles(iter.Value(), visited, depth+1) {
				return true
			}
		}
		return false
	case reflect.String, reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64, reflect.Complex64, reflect.Complex128,
		reflect.Invalid, reflect.Chan, reflect.Func, reflect.UnsafePointer:
		return false
	case reflect.Struct:
		// handled below
	}

	t := rv.Type()
	// A struct field of type File is caught here: the switch above only falls
	// through for Struct kinds, and File is the one struct type that means
	// multipart. A []File slice never reaches this line — slices are handled by
	// the Slice case's per-element recursion above.
	if t == reflect.TypeOf(File{}) {
		return true
	}

	if rv.Kind() != reflect.Struct {
		return false
	}

	// Guard against recursive types (e.g. a struct containing itself via a
	// pointer) using a call-stack visited set: a type is skipped only while it
	// is an ancestor of the current path, not merely because it was scanned
	// elsewhere. This keeps sibling branches independent — the same struct type
	// appearing twice with different interface-typed content must be checked
	// both times (the dynamic value can differ).
	if visited[t] {
		return false
	}
	visited[t] = true
	found := scanForFilesStruct(rv, visited, t, depth)
	delete(visited, t)
	return found
}

// scanForFilesStruct scans the exported fields of a struct value. It is split
// out so visit tracking can be released on every return path.
func scanForFilesStruct(rv reflect.Value, visited map[reflect.Type]bool, t reflect.Type, depth int) bool {
	for i := range t.NumField() {
		if !t.Field(i).IsExported() {
			continue
		}
		if scanForFiles(rv.Field(i), visited, depth+1) {
			return true
		}
	}
	return false
}
