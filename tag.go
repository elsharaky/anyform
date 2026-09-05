package anyform

import (
	"reflect"
	"strings"
	"sync"
)

// defaultTagPriority defines the default tag resolution order for marshalling.
// The first tag found on a struct field is used as the form key.
var defaultTagPriority = []string{"form", "json", "xml", "protobuf"}

// tagOptions holds parsed tag options.
type tagOptions struct {
	Name       string
	Skip       bool
	OmitEmpty  bool
	Required   bool
	Default    string
	HasDefault bool
}

// tagResolver inspects struct tags and resolves field names using a
// configurable priority order.
type tagResolver struct {
	priority []string
}

// newTagResolver creates a resolver with the given tag priority order.
// If no priorities are provided, the default (form > json > xml > protobuf) is used.
func newTagResolver(priority ...string) *tagResolver {
	p := priority
	if len(p) == 0 {
		p = defaultTagPriority
	}
	return &tagResolver{priority: p}
}

// priorities returns the tag priority order used by this resolver.
func (r *tagResolver) priorities() []string {
	out := make([]string, len(r.priority))
	copy(out, r.priority)
	return out
}

// marshalFieldName returns the form key for a struct field and whether the
// field should be skipped.
// It checks tags in priority order; returns the first match,
// or the exported Go field name as a last resort.
func (r *tagResolver) marshalFieldName(sf reflect.StructField) (name string, skip bool) {
	if !sf.IsExported() {
		return "", true
	}

	opts, found := r.firstExistingTag(sf)
	if found {
		if opts.Skip {
			return "", true
		}
		if opts.Name != "" {
			return opts.Name, false
		}
	}

	return sf.Name, false
}

// marshalFieldOptions returns the parsed tag options for the first matching
// tag in priority order. Returns a zero-value options if no tag is found.
func (r *tagResolver) marshalFieldOptions(sf reflect.StructField) tagOptions {
	opts, found := r.firstExistingTag(sf)
	if !found {
		return tagOptions{Name: sf.Name}
	}
	return opts
}

// firstExistingTag returns the options for the first tag in priority order
// that actually exists on the struct field. The bool is false if no tag exists.
func (r *tagResolver) firstExistingTag(sf reflect.StructField) (tagOptions, bool) {
	for _, tag := range r.priority {
		val, ok := sf.Tag.Lookup(tag)
		if !ok {
			continue
		}
		return parseTagOptions(val), true
	}
	return tagOptions{}, false
}

// unmarshalTagNames returns all non-skip tag names for a field, in priority
// order, plus the Go field name as a fallback. If the field is skipped (first
// tag in priority is "-"), skip is returned true and names is nil.
func (r *tagResolver) unmarshalTagNames(sf reflect.StructField) (names []string, skip bool) {
	for _, tag := range r.priority {
		val, ok := sf.Tag.Lookup(tag)
		if !ok {
			continue
		}
		opts := parseTagOptions(val)
		if opts.Skip {
			return nil, true
		}
		if opts.Name != "" {
			names = append(names, opts.Name)
		}
	}
	// Always include the Go field name as a fallback (matches buildUnmarshalIndex).
	names = append(names, sf.Name)
	return names, false
}

// isSkipped reports whether a field should be excluded from form handling.
// Anonymous embedded struct fields are never skipped at this level (they are
// recursed into by callers).
func (r *tagResolver) isSkipped(sf reflect.StructField) bool {
	if sf.Anonymous {
		return false
	}
	if !sf.IsExported() {
		return true
	}
	opts, found := r.firstExistingTag(sf)
	if !found {
		return false
	}
	return opts.Skip
}

// parseTagOptions parses a struct tag value like "name,omitempty,required,default:foo".
// It also handles protobuf-style tags where the field name is in "name=xxx" format
// (e.g., "protobuf:\"bytes,5,opt,name=proto_field,proto3,omitempty\"").
func parseTagOptions(tag string) tagOptions {
	opts := tagOptions{}

	parts := strings.Split(tag, ",")
	if len(parts) == 0 {
		return opts
	}

	// Check if this is a protobuf-style tag by looking for "name=" in any part.
	if protobufName, ok := parseProtobufFieldName(parts); ok {
		opts.Name = protobufName
		for _, part := range parts {
			if part == "omitempty" {
				opts.OmitEmpty = true
			}
		}
		return opts
	}

	opts.Name = parts[0]
	if opts.Name == "-" {
		opts.Skip = true
		return opts
	}

	for _, part := range parts[1:] {
		switch {
		case part == "omitempty":
			opts.OmitEmpty = true
		case part == "required":
			opts.Required = true
		case strings.HasPrefix(part, "default:"):
			opts.Default = strings.TrimPrefix(part, "default:")
			opts.HasDefault = true
		}
	}

	return opts
}

// parseProtobufFieldName checks if the tag parts look like a protobuf tag
// (contains a "name=xxx" part) and returns the field name.
// A protobuf tag always starts with a wire type (varint, fixed32, fixed64,
// bytes, etc.) followed by a field number, then opt/req/rep, then name=xxx.
func parseProtobufFieldName(parts []string) (string, bool) {
	if len(parts) < 3 {
		return "", false
	}

	// Protobuf wire types that appear as the first element.
	protoWireTypes := map[string]bool{
		"varint":  true,
		"fixed32": true,
		"fixed64": true,
		"bytes":   true,
		"group":   true,
	}

	if !protoWireTypes[parts[0]] {
		return "", false
	}

	for _, part := range parts {
		if strings.HasPrefix(part, "name=") {
			return strings.TrimPrefix(part, "name="), true
		}
	}
	return "", false
}

// unmarshalIndex is the resolved tag→field map for a struct type, plus every
// key that is ambiguous — resolving to more than one distinct field (e.g. an
// embedded struct field and an outer field sharing a tag name). An ambiguous
// key is a struct-design error: unmarshal rejects it instead of writing one
// field's payload into a sibling.
type unmarshalIndex struct {
	fields    map[string]reflect.StructField
	ambiguous map[string]bool
}

// unmarshalIndexKey identifies a built unmarshal index in the shared cache.
// The tag priority is part of the key because the index is priority-dependent.
type unmarshalIndexKey struct {
	t        reflect.Type
	priority string
}

// unmarshalIndexCache caches tag→field indexes built from struct types, the
// way encoding/json caches its type fields. Building an index is pure
// reflection and never mutates the type, so results are safe to share across
// all decoders and goroutines.
var unmarshalIndexCache sync.Map // key: unmarshalIndexKey, value: unmarshalIndex

// buildUnmarshalIndex builds a map from every possible tag value and Go field name
// to the corresponding struct field, for efficient lookup during unmarshalling.
// It checks all tags in priority order and registers every non-skip name.
// Results are cached per (type, tag priority) and reused across calls.
func (r *tagResolver) buildUnmarshalIndex(t reflect.Type) unmarshalIndex {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return unmarshalIndex{}
	}

	key := unmarshalIndexKey{t: t, priority: strings.Join(r.priority, ",")}
	idx, ok := func() (unmarshalIndex, bool) {
		cached, ok := unmarshalIndexCache.Load(key)
		if !ok {
			return unmarshalIndex{}, false
		}
		idx, _ := cached.(unmarshalIndex)
		return idx, true
	}()
	if ok {
		return idx
	}

	index := r.buildUnmarshalIndexUncached(t)
	actual, _ := unmarshalIndexCache.LoadOrStore(key, index)
	idx, _ = actual.(unmarshalIndex)
	return idx
}

// buildUnmarshalIndexUncached performs the actual reflection walk. Callers
// should use buildUnmarshalIndex, which caches the result per struct type.
func (r *tagResolver) buildUnmarshalIndexUncached(t reflect.Type) unmarshalIndex {
	fields := make(map[string]reflect.StructField)
	ambiguous := make(map[string]bool)

	// register maps a name to a field. If the name is already taken by a
	// DIFFERENT physical field (its index path differs), the name is
	// ambiguous; the same field re-registering under other tags is not.
	register := func(name string, sf reflect.StructField) {
		if prev, ok := fields[name]; ok && !reflect.DeepEqual(prev.Index, sf.Index) {
			ambiguous[name] = true
		}
		fields[name] = sf
	}

	for i := range t.NumField() {
		sf := t.Field(i)

		// Flatten anonymous embedded structs into the parent namespace.
		// Handle before the export check because anonymous fields use the
		// (possibly lowercase) type name.
		if sf.Anonymous && sf.Type.Kind() == reflect.Struct {
			inner := r.buildUnmarshalIndex(sf.Type)
			for k, v := range inner.fields {
				// The promoted field's Index is relative to the EMBEDDED type;
				// prepend this anonymous field's own index so that a later
				// FieldByIndex(v.Index) resolves on the parent struct. Copy
				// into a fresh slice: inner (and its entries) live in the shared
				// cache, and the cache entry must never be mutated.
				idx := make([]int, 0, len(v.Index)+len(sf.Index))
				idx = append(idx, sf.Index...)
				idx = append(idx, v.Index...)
				v.Index = idx
				register(k, v)
			}
			for k := range inner.ambiguous {
				ambiguous[k] = true
			}
			continue
		}

		if !sf.IsExported() {
			continue
		}

		if r.isSkipped(sf) {
			continue
		}

		for _, tag := range r.priority {
			val, ok := sf.Tag.Lookup(tag)
			if !ok {
				continue
			}
			opts := parseTagOptions(val)
			if opts.Name != "" {
				register(opts.Name, sf)
			}
		}

		register(sf.Name, sf)
	}

	return unmarshalIndex{fields: fields, ambiguous: ambiguous}
}
