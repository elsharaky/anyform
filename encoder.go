package anyform

import (
	"bytes"
	"encoding"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"reflect"
	"strconv"
	"time"
)

// Encoder marshals Go structs into url.Values or multipart form data.
// It is safe for concurrent use after construction.
type Encoder struct {
	cfg      *config
	resolver *tagResolver
}

// NewEncoder creates an Encoder with the given options.
func NewEncoder(opts ...Option) *Encoder {
	cfg := defaultConfig()
	for _, opt := range opts {
		if opt != nil {
			opt(cfg)
		}
	}
	return &Encoder{
		cfg:      cfg,
		resolver: newTagResolver(cfg.tagPriority...),
	}
}

// Marshal converts a struct (or pointer to struct) into url.Values.
// Returns ErrFileNotSupported if a File field is encountered; use
// MarshalMultipart for file support.
func (e *Encoder) Marshal(v any) (url.Values, error) {
	vals := make(url.Values)
	rv, err := addressableValue(v)
	if err != nil {
		return nil, &EncodingError{Err: err}
	}
	if err := e.encodeStruct(rv, "", vals, 0); err != nil {
		return nil, err
	}
	return vals, nil
}

// MarshalMultipart converts a struct into multipart form data, supporting
// File fields. The function returns the raw multipart body and its boundary.
func (e *Encoder) MarshalMultipart(v any) (body []byte, contentType string, err error) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	defer func() {
		_ = mw.Close()
	}()

	rv, derr := addressableValue(v)
	if derr != nil {
		return nil, "", &EncodingError{Err: derr}
	}
	if rv.Kind() != reflect.Struct {
		return nil, "", &EncodingError{Err: ErrNotStruct}
	}

	// Encode using multipart writer directly.
	if err := e.encodeStructMultipart(rv, "", mw, 0); err != nil {
		return nil, "", err
	}
	if err := mw.Close(); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), mw.FormDataContentType(), nil
}

// addressableValue returns an addressable reflect.Value for the struct
// described by v, dereferencing pointer layers. It returns ErrNotStruct if the
// underlying value is not a struct, and ErrNilPointer for a nil pointer.
func addressableValue(v any) (reflect.Value, error) {
	rv := reflect.ValueOf(v)
	if !rv.IsValid() {
		return reflect.Value{}, ErrNilPointer
	}
	if rv.Kind() == reflect.Pointer && rv.IsNil() {
		return reflect.Value{}, ErrNilPointer
	}
	for rv.Kind() == reflect.Pointer {
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return reflect.Value{}, ErrNotStruct
	}
	// Make an addressable copy so unexported anonymous fields can be inspected.
	addr := reflect.New(rv.Type())
	addr.Elem().Set(rv)
	return addr.Elem(), nil
}

// encodeStruct writes all scalar fields of a struct to vals.
func (e *Encoder) encodeStruct(rv reflect.Value, prefix string, vals url.Values, depth int) error {
	if depth > e.cfg.maxDepth {
		return &EncodingError{FieldPath: prefix, Err: ErrMaxDepthExceeded}
	}

	rt := rv.Type()
	for i := range rt.NumField() {
		sf := rt.Field(i)

		// Anonymous embedded struct: flatten fields into parent namespace.
		// Handle before export check (anonymous fields use camelCase type name).
		if sf.Anonymous && sf.Type.Kind() == reflect.Struct {
			fieldVal := rv.Field(i)
			if err := e.encodeStruct(fieldVal, prefix, vals, depth+1); err != nil {
				return err
			}
			continue
		}

		if !sf.IsExported() {
			continue
		}

		name, skip := e.resolver.marshalFieldName(sf)
		if skip {
			continue
		}

		fieldVal := rv.Field(i)
		fullKey := name
		if prefix != "" {
			fullKey = prefix + "." + name
		}

		opts := e.resolver.marshalFieldOptions(sf)
		if opts.OmitEmpty && isEmpty(fieldVal) {
			continue
		}
		if e.cfg.zeroEmpty && isEmpty(fieldVal) {
			continue
		}

		if err := e.encodeField(fieldVal, fullKey, vals, depth+1); err != nil {
			return err
		}
	}
	return nil
}

// encodeField writes a single field's value to vals, recursing for containers.
func (e *Encoder) encodeField(rv reflect.Value, key string, vals url.Values, depth int) error {
	if !rv.IsValid() {
		return nil
	}
	if depth > e.cfg.maxDepth {
		return &EncodingError{FieldPath: key, Err: ErrMaxDepthExceeded}
	}

	// Dereference pointers.
	if rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return nil
		}
		rv = rv.Elem()
	}

	t := rv.Type()

	// File fields require multipart.
	if t == reflect.TypeOf(File{}) || t == reflect.TypeOf([]File{}) {
		return &EncodingError{FieldPath: key, Err: ErrFileNotSupported}
	}

	// Custom converters take priority.
	if conv, ok := e.cfg.converters[t]; ok {
		s, err := conv.Marshal(rv)
		if err != nil {
			return &EncodingError{FieldPath: key, Err: err}
		}
		vals.Add(key, s)
		return nil
	}

	// Handle time.Time before the generic TextMarshaler branch so that a
	// custom layout via WithTimeLayout is respected.
	if t == reflect.TypeOf(time.Time{}) {
		tv := timeConverter{layout: e.cfg.timeLayout}
		s, err := tv.Marshal(rv)
		if err != nil {
			return &EncodingError{FieldPath: key, Err: err}
		}
		if s != "" {
			vals.Add(key, s)
		}
		return nil
	}

	// Handle configured text marshaler support.
	if e.cfg.textAware {
		if tm, ok := rv.Interface().(encoding.TextMarshaler); ok && t.Kind() != reflect.String {
			b, err := tm.MarshalText()
			if err != nil {
				return &EncodingError{FieldPath: key, Err: err}
			}
			vals.Add(key, string(b))
			return nil
		}
	}

	switch rv.Kind() {
	case reflect.Struct:
		// Nested struct: prefix children with key.
		return e.encodeStruct(rv, key, vals, depth+1)
	case reflect.Slice, reflect.Array:
		return e.encodeSlice(rv, key, vals, depth+1)
	case reflect.Map:
		return e.encodeMap(rv, key, vals, depth+1)
	case reflect.Bool, reflect.String,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64,
		reflect.Complex64, reflect.Complex128:
		vals.Add(key, formatScalar(rv))
		return nil
	case reflect.Interface:
		if rv.IsNil() {
			return nil
		}
		return e.encodeField(rv.Elem(), key, vals, depth+1)
	default:
		// Fallback: use fmt.Sprint for unusual kinds.
		vals.Add(key, fmt.Sprint(rv.Interface()))
		return nil
	}
}

func (e *Encoder) encodeSlice(rv reflect.Value, key string, vals url.Values, depth int) error {
	for i := range rv.Len() {
		elem := rv.Index(i)
		idxKey := key + "[" + strconv.Itoa(i) + "]"
		if err := e.encodeField(elem, idxKey, vals, depth+1); err != nil {
			return err
		}
	}
	return nil
}

func (e *Encoder) encodeMap(rv reflect.Value, key string, vals url.Values, depth int) error {
	iter := rv.MapRange()
	for iter.Next() {
		mapKey := iter.Key()
		var keyStr string
		if mapKey.Kind() == reflect.String {
			keyStr = mapKey.String()
		} else {
			keyStr = formatScalar(mapKey)
		}
		nestedKey := key + "[" + keyStr + "]"
		if err := e.encodeField(iter.Value(), nestedKey, vals, depth+1); err != nil {
			return err
		}
	}
	return nil
}

// encodeStructMultipart writes fields to a multipart writer, handling File fields.
func (e *Encoder) encodeStructMultipart(rv reflect.Value, prefix string, mw *multipart.Writer, depth int) error {
	if depth > e.cfg.maxDepth {
		return &EncodingError{Err: ErrMaxDepthExceeded}
	}

	rt := rv.Type()
	for i := range rt.NumField() {
		sf := rt.Field(i)

		if sf.Anonymous && sf.Type.Kind() == reflect.Struct {
			fieldVal := rv.Field(i)
			if err := e.encodeStructMultipart(fieldVal, prefix, mw, depth+1); err != nil {
				return err
			}
			continue
		}

		if !sf.IsExported() {
			continue
		}

		name, skip := e.resolver.marshalFieldName(sf)
		if skip {
			continue
		}

		fullKey := name
		if prefix != "" {
			fullKey = prefix + "." + name
		}

		opts := e.resolver.marshalFieldOptions(sf)
		if opts.OmitEmpty && isEmpty(rv.Field(i)) {
			continue
		}
		if e.cfg.zeroEmpty && isEmpty(rv.Field(i)) {
			continue
		}

		if err := e.encodeFieldMultipart(rv.Field(i), fullKey, mw, depth+1); err != nil {
			return err
		}
	}
	return nil
}

// empty value helper
func isEmpty(rv reflect.Value) bool {
	if !rv.IsValid() {
		return true
	}
	if rv.Kind() == reflect.Pointer || rv.Kind() == reflect.Interface {
		return rv.IsNil()
	}
	return rv.IsZero()
}

func (e *Encoder) encodeFieldMultipart(rv reflect.Value, key string, mw *multipart.Writer, depth int) error {
	if !rv.IsValid() {
		return nil
	}
	if depth > e.cfg.maxDepth {
		return &EncodingError{FieldPath: key, Err: ErrMaxDepthExceeded}
	}
	if rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return nil
		}
		rv = rv.Elem()
	}

	t := rv.Type()

	// Files written as multipart parts.
	if t == reflect.TypeOf(File{}) {
		f, ok := rv.Interface().(File)
		if !ok {
			return &EncodingError{FieldPath: key, Err: errors.New("expected File")}
		}
		return writeFilePart(mw, key, f)
	}
	if t == reflect.TypeOf([]File{}) {
		fs, ok := rv.Interface().([]File)
		if !ok {
			return &EncodingError{FieldPath: key, Err: errors.New("expected []File")}
		}
		for _, f := range fs {
			if err := writeFilePart(mw, key, f); err != nil {
				return err
			}
		}
		return nil
	}

	// Custom converters.
	if conv, ok := e.cfg.converters[t]; ok {
		s, err := conv.Marshal(rv)
		if err != nil {
			return &EncodingError{FieldPath: key, Err: err}
		}
		return writeStringPart(mw, key, s)
	}

	// time.Time.
	if t == reflect.TypeOf(time.Time{}) {
		tv := timeConverter{layout: e.cfg.timeLayout}
		s, err := tv.Marshal(rv)
		if err != nil {
			return &EncodingError{FieldPath: key, Err: err}
		}
		if s != "" {
			return writeStringPart(mw, key, s)
		}
		return nil
	}

	switch rv.Kind() {
	case reflect.String:
		return writeStringPart(mw, key, rv.String())
	case reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64, reflect.Complex64, reflect.Complex128:
		return writeStringPart(mw, key, formatScalar(rv))
	case reflect.Slice, reflect.Array:
		for i := range rv.Len() {
			elem := rv.Index(i)
			idxKey := key + "[" + strconv.Itoa(i) + "]"
			if err := e.encodeFieldMultipart(elem, idxKey, mw, depth+1); err != nil {
				return err
			}
		}
		return nil
	case reflect.Map:
		iter := rv.MapRange()
		for iter.Next() {
			k := iter.Key()
			var keyStr string
			if k.Kind() == reflect.String {
				keyStr = k.String()
			} else {
				keyStr = formatScalar(k)
			}
			if err := e.encodeFieldMultipart(iter.Value(), key+"["+keyStr+"]", mw, depth+1); err != nil {
				return err
			}
		}
		return nil
	case reflect.Struct:
		return e.encodeStructMultipart(rv, key, mw, depth+1)
	case reflect.Interface:
		if rv.IsNil() {
			return nil
		}
		return e.encodeFieldMultipart(rv.Elem(), key, mw, depth+1)
	default:
		return writeStringPart(mw, key, fmt.Sprint(rv.Interface()))
	}
}

func writeFilePart(mw *multipart.Writer, key string, f File) error {
	h := make(textproto.MIMEHeader)
	h["Content-Disposition"] = []string{fmt.Sprintf(`form-data; name=%q; filename=%q`, key, f.Filename)}
	if f.ContentType != "" {
		h["Content-Type"] = []string{f.ContentType}
	} else {
		h["Content-Type"] = []string{http.DetectContentType(f.Content)}
	}
	w, err := mw.CreatePart(h)
	if err != nil {
		return &EncodingError{FieldPath: key, Err: err}
	}
	if _, err := w.Write(f.Content); err != nil {
		return &EncodingError{FieldPath: key, Err: err}
	}
	return nil
}

func writeStringPart(mw *multipart.Writer, key, value string) error {
	w, err := mw.CreateFormField(key)
	if err != nil {
		return &EncodingError{FieldPath: key, Err: err}
	}
	if _, err := w.Write([]byte(value)); err != nil {
		return &EncodingError{FieldPath: key, Err: err}
	}
	return nil
}
