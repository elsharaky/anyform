package anyform

import (
	"encoding"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"time"
)

// keyToken is a parsed segment of a submitted form key.
type keyToken struct {
	kind string // "field", "index", "mapkey"
	name string
}

// parseKeyPath splits a form key into its path tokens.
// Examples:
//
//	"name"            -> [{field name}]
//	"address.city"    -> [{field address} {field city}]
//	"items[0].name"   -> [{field items} {index 0} {field name}]
//	"attr[key]"       -> [{field attr} {mapkey key}]
//	"matrix[0][1]"    -> [{field matrix} {index 0} {index 1}]
//
// Returns the base field name and the remaining tokens.
func parseKeyPath(key string) (base string, rest []keyToken) {
	var tokens []keyToken
	var name strings.Builder
	i := 0
	flush := func() {
		if name.Len() > 0 {
			tokens = append(tokens, keyToken{kind: "field", name: name.String()})
			name.Reset()
		}
	}

	for i < len(key) {
		c := key[i]
		switch c {
		case '.':
			flush()
			i++
		case '[':
			flush()
			end := strings.IndexByte(key[i:], ']')
			if end < 0 {
				name.WriteString(key[i:])
				flush()
				i = len(key)
				continue
			}
			inner := key[i+1 : i+end]
			if n, err := strconv.Atoi(inner); err == nil {
				tokens = append(tokens, keyToken{kind: "index", name: strconv.Itoa(n)})
			} else {
				tokens = append(tokens, keyToken{kind: "mapkey", name: inner})
			}
			i = i + end + 1
		default:
			name.WriteByte(c)
			i++
		}
	}
	flush()

	if len(tokens) == 0 {
		return "", nil
	}
	return tokens[0].name, tokens[1:]
}

// Decoder unmarshals url.Values or multipart form data into Go structs.
// It is safe for concurrent use after construction.
type Decoder struct {
	cfg      *config
	resolver *tagResolver
}

// NewDecoder creates a Decoder with the given options.
func NewDecoder(opts ...Option) *Decoder {
	cfg := defaultConfig()
	for _, opt := range opts {
		if opt != nil {
			opt(cfg)
		}
	}
	return &Decoder{
		cfg:      cfg,
		resolver: newTagResolver(cfg.tagPriority...),
	}
}

// Unmarshal converts url.Values into the struct pointed to by v.
// v must be a non-nil pointer to a struct.
func (d *Decoder) Unmarshal(values url.Values, v any) error {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return &DecodingError{Err: errors.New("expected non-nil pointer to struct")}
	}
	elem := rv.Elem()
	if elem.Kind() != reflect.Struct {
		return &DecodingError{Err: ErrNotStruct}
	}
	if err := d.unmarshalValues(values, elem, 0); err != nil {
		return err
	}
	return d.applyDefaultsAndRequired(elem, valuesKeys(values), 0)
}

// UnmarshalMultipart parses an http.Request's multipart form into the struct
// pointed to by v. File fields of type File/[]File are populated from the
// multipart file parts. The request must have been parsed with
// ParseMultipartForm beforehand.
func (d *Decoder) UnmarshalMultipart(r *http.Request, v any) error {
	return d.UnmarshalMultipartForm(r.MultipartForm, v)
}

// UnmarshalMultipartForm parses a multipart.Form into the struct pointed to by v,
// populating both scalar and File fields.
func (d *Decoder) UnmarshalMultipartForm(mf *multipart.Form, v any) error {
	if mf == nil {
		return &DecodingError{Err: errors.New("anyform: nil multipart form")}
	}

	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return &DecodingError{Err: errors.New("expected non-nil pointer to struct")}
	}
	elem := rv.Elem()
	if elem.Kind() != reflect.Struct {
		return &DecodingError{Err: ErrNotStruct}
	}

	values := url.Values(mf.Value)
	if err := d.unmarshalValues(values, elem, 0); err != nil {
		return err
	}
	if err := d.unmarshalFiles(mf, elem); err != nil {
		return err
	}
	return d.applyDefaultsAndRequired(elem, multipartKeys(mf), 0)
}

// unmarshalValues iterates over the submitted keys and assigns values,
// building a fresh field index for the struct level.
func (d *Decoder) unmarshalValues(values url.Values, dst reflect.Value, depth int) error {
	if depth > d.cfg.maxDepth {
		return &DecodingError{Err: ErrMaxDepthExceeded}
	}

	index := d.resolver.buildUnmarshalIndex(dst.Type())

	for key, vals := range values {
		base, rest := parseKeyPath(key)
		field, ok := index[base]
		if !ok {
			if d.cfg.strict {
				return &DecodingError{Key: key, Err: fmt.Errorf("unknown field %q", base)}
			}
			continue
		}

		fieldVal := dst.FieldByIndex(field.Index)
		if !fieldVal.CanSet() {
			continue
		}

		if err := d.decodePath(fieldVal, rest, vals, depth+1); err != nil {
			return err
		}
	}

	return nil
}

// decodePath walks the parsed key tokens and assigns the leaf value(s).
func (d *Decoder) decodePath(field reflect.Value, rest []keyToken, vals []string, depth int) error {
	if depth > d.cfg.maxDepth {
		return &DecodingError{Err: ErrMaxDepthExceeded}
	}

	// Dereference pointers as we descend.
	if field.Kind() == reflect.Pointer {
		if field.IsNil() {
			field.Set(reflect.New(field.Type().Elem()))
		}
		field = field.Elem()
	}

	if len(rest) == 0 {
		// Leaf: assign value(s) to this field.
		return d.assignLeaf(field, vals)
	}

	head := rest[0]
	tail := rest[1:]

	switch head.kind {
	case "field":
		// Descend into a named sub-field.
		switch field.Kind() {
		case reflect.Struct:
			if field.Type() == reflect.TypeOf(File{}) {
				return &DecodingError{Err: errors.New("cannot descend into File; use UnmarshalMultipartForm")}
			}
			childIndex := d.resolver.buildUnmarshalIndex(field.Type())
			child, ok := childIndex[head.name]
			if !ok {
				return &DecodingError{FieldPath: head.name, Err: errors.New("unknown field")}
			}
			childVal := field.FieldByIndex(child.Index)
			return d.decodePath(childVal, tail, vals, depth+1)
		case reflect.Slice, reflect.Array:
			// slice of struct accessed without index (rare) — treat as append
			return d.decodePath(field, tail, vals, depth+1)
		case reflect.Map:
			// map accessed via .field — not supported; skip
			return nil
		default:
			return &DecodingError{FieldPath: head.name, Err: errors.New("cannot descend into scalar")}
		}

	case "index":
		if field.Kind() != reflect.Slice && field.Kind() != reflect.Array {
			return &DecodingError{FieldPath: head.name, Err: errors.New("index on non-indexable field")}
		}
		idx, err := strconv.Atoi(head.name)
		if err != nil {
			return &DecodingError{FieldPath: head.name, Err: fmt.Errorf("invalid index %q", head.name)}
		}
		for field.Len() <= idx {
			field.Set(reflect.Append(field, reflect.New(field.Type().Elem()).Elem()))
		}
		elem := field.Index(idx)
		return d.decodePath(elem, tail, vals, depth+1)

	case "mapkey":
		if field.Kind() != reflect.Map {
			return &DecodingError{FieldPath: head.name, Err: errors.New("mapkey on non-map field")}
		}
		if field.IsNil() {
			field.Set(reflect.MakeMap(field.Type()))
		}
		mapKey := reflect.New(field.Type().Key()).Elem()
		if err := d.assignScalarTo(mapKey, head.name); err != nil {
			return &DecodingError{FieldPath: head.name, Err: err}
		}

		if len(tail) == 0 {
			// Leaf map value.
			val := reflect.New(field.Type().Elem()).Elem()
			if err := d.assignLeaf(val, vals); err != nil {
				return &DecodingError{FieldPath: head.name, Err: err}
			}
			field.SetMapIndex(mapKey, val)
			return nil
		}

		// Map value is complex (struct/slice); descending requires a settable
		// value. Create it, decode, then set.
		val := reflect.New(field.Type().Elem()).Elem()
		if err := d.decodePath(val, tail, vals, depth+1); err != nil {
			return &DecodingError{FieldPath: head.name, Err: err}
		}
		field.SetMapIndex(mapKey, val)
		return nil

	default:
		return &DecodingError{FieldPath: head.name, Err: errors.New("unknown key token")}
	}
}

// assignLeaf assigns raw string values to a leaf field of any supported kind.
func (d *Decoder) assignLeaf(field reflect.Value, vals []string) error {
	if len(vals) == 0 {
		return nil
	}
	if field.Kind() == reflect.Pointer {
		if field.IsNil() {
			field.Set(reflect.New(field.Type().Elem()))
		}
		field = field.Elem()
	}

	// Registered converters take priority over kind-based handling, matching
	// the encoder. This matters for net.IP (slice kind) and url.URL (struct
	// kind), which otherwise fall into the slice/struct branches below.
	if conv, ok := d.cfg.converters[field.Type()]; ok {
		return conv.Unmarshal(vals[0], field)
	}

	switch field.Kind() {
	case reflect.Struct:
		if field.Type() == reflect.TypeOf(File{}) {
			return errors.New("cannot decode value part into File field: multipart file parts must include a filename")
		}
		if field.Type() == reflect.TypeOf(time.Time{}) {
			tv := timeConverter{layout: d.cfg.timeLayout}
			if err := tv.Unmarshal(vals[0], field); err != nil {
				return &DecodingError{FieldPath: "", Err: err}
			}
			return nil
		}
		// Nested struct as a single value — treat as flattening: ignore extra.
		return nil
	case reflect.Slice, reflect.Array:
		// A value part applied to a File element means the client sent the
		// part without a filename, so the multipart parser routed it to the
		// value path. Surface that instead of a raw "unsupported field kind".
		if field.Type().Elem() == reflect.TypeOf(File{}) {
			return errors.New("cannot decode value part into []File field: multipart file parts must include a filename")
		}
		// Repeated key: append each value as new element.
		if field.Kind() == reflect.Slice {
			for _, v := range vals {
				elem := reflect.New(field.Type().Elem()).Elem()
				if err := d.assignScalarTo(elem, v); err != nil {
					return &DecodingError{FieldPath: "", Err: err}
				}
				field.Set(reflect.Append(field, elem))
			}
			return nil
		}
		// Array: set by position.
		for i, v := range vals {
			if i >= field.Len() {
				break
			}
			if err := d.assignScalarTo(field.Index(i), v); err != nil {
				return &DecodingError{FieldPath: "", Err: err}
			}
		}
		return nil
	case reflect.Map:
		// A map reached directly without key -> unsupported.
		return &DecodingError{FieldPath: "", Err: errors.New("map requires bracket notation")}
	default:
		return d.assignScalarTo(field, vals[0])
	}
}

// assignScalarTo assigns a single string to a target field, honoring custom
// converters and TextUnmarshaler.
func (d *Decoder) assignScalarTo(field reflect.Value, value string) error {
	if field.CanAddr() {
		if tu, ok := field.Addr().Interface().(encoding.TextUnmarshaler); ok && d.cfg.textAware {
			return tu.UnmarshalText([]byte(value))
		}
	}
	if conv, ok := d.cfg.converters[field.Type()]; ok {
		return conv.Unmarshal(value, field)
	}
	return parseScalar(value, field)
}

// readFile reads a single multipart file header into a File, honoring the
// configured per-file size limit. The size is checked against the part's
// declared size before its content is read into memory.
func (d *Decoder) readFile(fh *multipart.FileHeader, name string) (File, error) {
	if d.cfg.maxFileSize > 0 && fh.Size > d.cfg.maxFileSize {
		return File{}, &DecodingError{FieldPath: name, Err: ErrFileTooLarge}
	}
	f, err := FileFromHeader(fh)
	if err != nil {
		return File{}, &DecodingError{FieldPath: name, Err: err}
	}
	// Safety net: a manually constructed FileHeader may carry Size == 0 even
	// though its read content exceeds the limit.
	if d.cfg.maxFileSize > 0 && int64(len(f.Content)) > d.cfg.maxFileSize {
		return File{}, &DecodingError{FieldPath: name, Err: ErrFileTooLarge}
	}
	return f, nil
}

// unmarshalFiles populates File and []File fields from multipart file parts.
func (d *Decoder) unmarshalFiles(mf *multipart.Form, dst reflect.Value) error {
	if len(mf.File) == 0 {
		return nil
	}

	rt := dst.Type()
	consumed := make(map[string]bool, len(mf.File))
	for i := range rt.NumField() {
		sf := rt.Field(i)

		if sf.Anonymous && sf.Type.Kind() == reflect.Struct {
			if sf.Type == reflect.TypeOf(File{}) {
				continue
			}
			if err := d.unmarshalFiles(mf, dst.Field(i)); err != nil {
				return err
			}
			continue
		}

		if !sf.IsExported() {
			continue
		}

		// Resolve all tag names that can address this field (form, json, xml,
		// protobuf, plus the Go field name) — matching how value fields work.
		names, skip := d.resolver.unmarshalTagNames(sf)
		if skip {
			continue
		}

		fieldVal := dst.Field(i)
		if !fieldVal.CanSet() {
			continue
		}

		var files []*multipart.FileHeader
		var matchedName string
		for _, name := range names {
			if got, ok := mf.File[name]; ok {
				files = got
				matchedName = name
				break
			}
		}

		switch {
		case fieldVal.Type() == reflect.TypeOf(File{}):
			if len(files) > 0 {
				f, err := d.readFile(files[0], matchedName)
				if err != nil {
					return err
				}
				fieldVal.Set(reflect.ValueOf(f))
				consumed[matchedName] = true
			}
		case fieldVal.Type() == reflect.TypeOf([]File{}):
			if len(files) == 0 {
				continue
			}
			out := make([]File, 0, len(files))
			for _, fh := range files {
				f, err := d.readFile(fh, matchedName)
				if err != nil {
					return err
				}
				out = append(out, f)
			}
			fieldVal.Set(reflect.ValueOf(out))
			consumed[matchedName] = true
		case fieldVal.Kind() == reflect.Pointer && fieldVal.Type().Elem() == reflect.TypeOf(File{}):
			if len(files) == 0 {
				continue
			}
			f, err := d.readFile(files[0], matchedName)
			if err != nil {
				return err
			}
			if fieldVal.IsNil() {
				fieldVal.Set(reflect.New(reflect.TypeOf(File{})))
			}
			fieldVal.Elem().Set(reflect.ValueOf(f))
			consumed[matchedName] = true
		}
	}

	// Under strict mode, any multipart file part that did not map to a File
	// field is an error instead of being silently dropped.
	if d.cfg.strict {
		for part := range mf.File {
			if !consumed[part] {
				return &DecodingError{Key: part, Err: fmt.Errorf("unknown field %q", part)}
			}
		}
	}

	return nil
}

// valuesKeys collects the base keys submitted in a url.Values.
func valuesKeys(values url.Values) map[string]bool {
	keys := make(map[string]bool, len(values))
	for k := range values {
		base, _ := parseKeyPath(k)
		keys[base] = true
	}
	return keys
}

// multipartKeys collects the base keys submitted in a multipart form,
// combining value fields and file fields.
func multipartKeys(mf *multipart.Form) map[string]bool {
	keys := make(map[string]bool, len(mf.Value)+len(mf.File))
	for k := range mf.Value {
		base, _ := parseKeyPath(k)
		keys[base] = true
	}
	for k := range mf.File {
		base, _ := parseKeyPath(k)
		keys[base] = true
	}
	return keys
}

// applyDefaultsAndRequired walks the destination struct after unmarshalling,
// setting default values for fields not provided and enforcing required fields.
// A field is considered provided when a submitted key matches its resolved name
// exactly or as a "name." / "name[" prefix (covering nested and indexed keys).
func (d *Decoder) applyDefaultsAndRequired(dst reflect.Value, submitted map[string]bool, depth int) error {
	if dst.Kind() == reflect.Pointer {
		if dst.IsNil() {
			return nil
		}
		dst = dst.Elem()
	}
	if dst.Kind() != reflect.Struct || depth > d.cfg.maxDepth {
		return nil
	}

	rt := dst.Type()
	for i := range rt.NumField() {
		sf := rt.Field(i)
		fieldVal := dst.Field(i)

		if sf.Anonymous && sf.Type.Kind() == reflect.Struct {
			if sf.Type == reflect.TypeOf(File{}) {
				continue
			}
			if err := d.applyDefaultsAndRequired(fieldVal, submitted, depth+1); err != nil {
				return err
			}
			continue
		}

		if !sf.IsExported() {
			continue
		}

		name, skip := d.resolver.marshalFieldName(sf)
		if skip {
			continue
		}

		opts := d.resolver.marshalFieldOptions(sf)

		// Recurse into nested structs so inner defaults/required apply too.
		switch {
		case fieldVal.Kind() == reflect.Pointer &&
			fieldVal.Type().Elem().Kind() == reflect.Struct &&
			fieldVal.Type().Elem() != reflect.TypeOf(File{}) &&
			!fieldVal.IsNil():
			if err := d.applyDefaultsAndRequired(fieldVal, submitted, depth+1); err != nil {
				return err
			}
		case fieldVal.Kind() == reflect.Struct &&
			fieldVal.Type() != reflect.TypeOf(File{}) &&
			fieldVal.Type() != reflect.TypeOf(time.Time{}):
			if err := d.applyDefaultsAndRequired(fieldVal, submitted, depth+1); err != nil {
				return err
			}
		}

		if keyProvided(submitted, name) {
			continue
		}

		if opts.Required {
			return &DecodingError{Key: name, Err: ErrMissingRequired}
		}
		if opts.HasDefault && isDefaultable(fieldVal.Type()) {
			if err := d.assignScalarTo(fieldVal, opts.Default); err != nil {
				return &DecodingError{Key: name, Err: err}
			}
		}
	}

	return nil
}

// isDefaultable reports whether a type can receive a default value from a
// string. Pointers, slices, maps, and File types are intentionally excluded.
func isDefaultable(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.String, reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64, reflect.Complex64, reflect.Complex128:
		return true
	default:
		return false
	}
}

// keyProvided reports whether the submitted key set covers the given field name,
// either directly or as the prefix of a nested/indexed key.
func keyProvided(submitted map[string]bool, name string) bool {
	if submitted[name] {
		return true
	}
	dot := name + "."
	bracket := name + "["
	for k := range submitted {
		if len(k) >= len(dot) && k[:len(dot)] == dot {
			return true
		}
		if len(k) >= len(bracket) && k[:len(bracket)] == bracket {
			return true
		}
	}
	return false
}
