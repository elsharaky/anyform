package anyform

import (
	"bytes"
	"errors"
	"mime/multipart"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestParseKeyPath(t *testing.T) {
	tests := []struct {
		key  string
		base string
		rest []keyToken
	}{
		{"name", "name", nil},
		{"address.city", "address", []keyToken{{"field", "city"}}},
		{"items[0].name", "items", []keyToken{{"index", "0"}, {"field", "name"}}},
		{"attr[key]", "attr", []keyToken{{"mapkey", "key"}}},
		{"matrix[0][1]", "matrix", []keyToken{{"index", "0"}, {"index", "1"}}},
		{"a.b.c", "a", []keyToken{{"field", "b"}, {"field", "c"}}},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			base, rest := parseKeyPath(tt.key)
			if base != tt.base {
				t.Errorf("base = %q, want %q", base, tt.base)
			}
			if len(rest) != len(tt.rest) {
				t.Fatalf("rest = %+v, want %+v", rest, tt.rest)
			}
			for i := range rest {
				if rest[i] != tt.rest[i] {
					t.Errorf("rest[%d] = %+v, want %+v", i, rest[i], tt.rest[i])
				}
			}
		})
	}
}

type encodeBasic struct {
	Name  string `form:"name"`
	Age   int    `form:"age"`
	Score float64
	Tags  []string `form:"tags"`
}

func TestEncoder_Marshal_Basic(t *testing.T) {
	enc := NewEncoder()
	vals, err := enc.Marshal(encodeBasic{
		Name:  "alice",
		Age:   30,
		Score: 9.5,
		Tags:  []string{"a", "b"},
	})
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	expected := url.Values{
		"name":    {"alice"},
		"age":     {"30"},
		"Score":   {"9.5"},
		"tags[0]": {"a"},
		"tags[1]": {"b"},
	}
	for k, exp := range expected {
		if got := vals[k]; !reflect.DeepEqual(got, exp) {
			t.Errorf("key %q = %v, want %v", k, got, exp)
		}
	}
}

type encodeNested struct {
	Personal struct {
		Name  string `form:"name"`
		Email string `form:"email"`
	} `form:"personal"`
}

func TestEncoder_Marshal_Nested(t *testing.T) {
	enc := NewEncoder()
	in := encodeNested{}
	in.Personal.Name = "bob"
	in.Personal.Email = "bob@x.com"
	vals, err := enc.Marshal(in)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	if vals.Get("personal.name") != "bob" {
		t.Errorf("personal.name = %q", vals.Get("personal.name"))
	}
	if vals.Get("personal.email") != "bob@x.com" {
		t.Errorf("personal.email = %q", vals.Get("personal.email"))
	}
}

type encodeMaps struct {
	Attr map[string]string `form:"attr"`
}

func TestEncoder_Marshal_Map(t *testing.T) {
	enc := NewEncoder()
	vals, err := enc.Marshal(encodeMaps{Attr: map[string]string{"x": "1", "y": "2"}})
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	if vals.Get("attr[x]") != "1" {
		t.Errorf("attr[x] = %q", vals.Get("attr[x]"))
	}
	if vals.Get("attr[y]") != "2" {
		t.Errorf("attr[y] = %q", vals.Get("attr[y]"))
	}
}

type encodeOmit struct {
	A string `form:"a,omitempty"`
	B string `form:"b"`
}

func TestEncoder_Marshal_OmitEmpty(t *testing.T) {
	enc := NewEncoder()
	vals, err := enc.Marshal(encodeOmit{B: "x"})
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	if _, ok := vals["a"]; ok {
		t.Error("omitempty field should be omitted")
	}
	if vals.Get("b") != "x" {
		t.Errorf("b = %q", vals.Get("b"))
	}
}

type encodeTime struct {
	Created time.Time `form:"created"`
}

func TestEncoder_Marshal_Time(t *testing.T) {
	enc := NewEncoder()
	ts, _ := time.Parse(time.RFC3339, "2024-01-02T15:04:05Z")
	vals, err := enc.Marshal(encodeTime{Created: ts})
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	if vals.Get("created") != "2024-01-02T15:04:05Z" {
		t.Errorf("created = %q", vals.Get("created"))
	}
}

func TestEncoder_Marshal_Layout_Option(t *testing.T) {
	enc := NewEncoder(WithTimeLayout("2006-01-02"))
	ts, _ := time.Parse(time.RFC3339, "2024-01-02T15:04:05Z")
	vals, err := enc.Marshal(encodeTime{Created: ts})
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	if vals.Get("created") != "2024-01-02" {
		t.Errorf("created = %q", vals.Get("created"))
	}
}

type encodeEmbedded struct {
	encodeBasic
	Extra string `form:"extra"`
}

func TestEncoder_Marshal_Embedded(t *testing.T) {
	enc := NewEncoder()
	vals, err := enc.Marshal(encodeEmbedded{encodeBasic{Name: "n", Age: 1}, "e"})
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	if vals.Get("name") != "n" {
		t.Errorf("name = %q", vals.Get("name"))
	}
	if vals.Get("extra") != "e" {
		t.Errorf("extra = %q", vals.Get("extra"))
	}
}

func TestEncoder_Marshal_NonStruct(t *testing.T) {
	enc := NewEncoder()
	_, err := enc.Marshal(42)
	if err == nil {
		t.Fatal("expected error for non-struct")
	}
}

func TestEncoder_Marshal_Pointer(t *testing.T) {
	enc := NewEncoder()
	vals, err := enc.Marshal(&encodeBasic{Name: "p"})
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	if vals.Get("name") != "p" {
		t.Errorf("name = %q", vals.Get("name"))
	}
}

func TestEncoder_Marshal_FileError(t *testing.T) {
	enc := NewEncoder()
	in := struct {
		F File `form:"f"`
	}{}
	_, err := enc.Marshal(in)
	if err == nil {
		t.Fatal("expected file error in url.Values marshalling")
	}
}

type decodeBasic struct {
	Name  string `form:"name"`
	Age   int    `form:"age"`
	Email string `json:"user_email"`
}

func TestDecoder_Unmarshal_Basic(t *testing.T) {
	dec := NewDecoder()
	vals := url.Values{
		"name":       {"alice"},
		"age":        {"30"},
		"user_email": {"alice@x.com"},
	}
	var out decodeBasic
	if err := dec.Unmarshal(vals, &out); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if out.Name != "alice" || out.Age != 30 || out.Email != "alice@x.com" {
		t.Errorf("unexpected result: %+v", out)
	}
}

func TestDecoder_Unmarshal_TagPriority(t *testing.T) {
	dec := NewDecoder()
	vals := url.Values{
		"name": {"n"}, // matches form tag (Go name also)
	}
	var out decodeBasic
	if err := dec.Unmarshal(vals, &out); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if out.Name != "n" {
		t.Errorf("Name = %q", out.Name)
	}
}

type decodeNested struct {
	Personal struct {
		Name  string `form:"name"`
		Email string `form:"email"`
	} `form:"personal"`
}

func TestDecoder_Unmarshal_Nested(t *testing.T) {
	dec := NewDecoder()
	vals := url.Values{
		"personal.name":  {"bob"},
		"personal.email": {"bob@x.com"},
	}
	var out decodeNested
	if err := dec.Unmarshal(vals, &out); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if out.Personal.Name != "bob" || out.Personal.Email != "bob@x.com" {
		t.Errorf("unexpected result: %+v", out.Personal)
	}
}

type decodeSlice struct {
	Tags []string `form:"tags"`
}

func TestDecoder_Unmarshal_Slice_Indexed(t *testing.T) {
	dec := NewDecoder()
	vals := url.Values{
		"tags[0]": {"a"},
		"tags[1]": {"b"},
	}
	var out decodeSlice
	if err := dec.Unmarshal(vals, &out); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if !reflect.DeepEqual(out.Tags, []string{"a", "b"}) {
		t.Errorf("Tags = %v", out.Tags)
	}
}

func TestDecoder_Unmarshal_Slice_Repeated(t *testing.T) {
	dec := NewDecoder()
	vals := url.Values{
		"tags": {"a", "b", "c"},
	}
	var out decodeSlice
	if err := dec.Unmarshal(vals, &out); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if !reflect.DeepEqual(out.Tags, []string{"a", "b", "c"}) {
		t.Errorf("Tags = %v", out.Tags)
	}
}

type decodeMap struct {
	Attr map[string]string `form:"attr"`
}

func TestDecoder_Unmarshal_Map(t *testing.T) {
	dec := NewDecoder()
	vals := url.Values{
		"attr[x]": {"1"},
		"attr[y]": {"2"},
	}
	var out decodeMap
	if err := dec.Unmarshal(vals, &out); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if out.Attr["x"] != "1" || out.Attr["y"] != "2" {
		t.Errorf("Attr = %v", out.Attr)
	}
}

type decodeTime struct {
	Created time.Time `form:"created"`
}

func TestDecoder_Unmarshal_Time(t *testing.T) {
	dec := NewDecoder()
	vals := url.Values{"created": {"2024-01-02T15:04:05Z"}}
	var out decodeTime
	if err := dec.Unmarshal(vals, &out); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	expected, _ := time.Parse(time.RFC3339, "2024-01-02T15:04:05Z")
	if !out.Created.Equal(expected) {
		t.Errorf("Created = %v", out.Created)
	}
}

type decodePointers struct {
	Name *string `form:"name"`
}

func TestDecoder_Unmarshal_Pointer(t *testing.T) {
	dec := NewDecoder()
	vals := url.Values{"name": {"ptr"}}
	var out decodePointers
	if err := dec.Unmarshal(vals, &out); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if out.Name == nil || *out.Name != "ptr" {
		t.Errorf("Name = %v", out.Name)
	}
}

func TestDecoder_Unmarshal_Strict_Unknown(t *testing.T) {
	dec := NewDecoder(WithStrictUnmarshal(true))
	vals := url.Values{"unknown": {"x"}}
	var out decodeBasic
	if err := dec.Unmarshal(vals, &out); err == nil {
		t.Fatal("expected error in strict mode for unknown key")
	}
}

func TestDecoder_Unmarshal_Strict_Ignored(t *testing.T) {
	dec := NewDecoder()
	vals := url.Values{"unknown": {"x"}}
	var out decodeBasic
	if err := dec.Unmarshal(vals, &out); err != nil {
		t.Fatalf("unexpected error in non-strict mode: %v", err)
	}
}

func TestDecoder_Unmarshal_NonPointer(t *testing.T) {
	dec := NewDecoder()
	vals := url.Values{}
	var out decodeBasic
	if err := dec.Unmarshal(vals, out); err == nil {
		t.Fatal("expected error for non-pointer")
	}
}

type decodeSliceStruct struct {
	Items []struct {
		Name string `form:"name"`
	} `form:"items"`
}

func TestDecoder_Unmarshal_SliceOfStruct(t *testing.T) {
	dec := NewDecoder()
	vals := url.Values{
		"items[0].name": {"a"},
		"items[1].name": {"b"},
	}
	var out decodeSliceStruct
	if err := dec.Unmarshal(vals, &out); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if len(out.Items) != 2 {
		t.Fatalf("Items len = %d", len(out.Items))
	}
	if out.Items[0].Name != "a" || out.Items[1].Name != "b" {
		t.Errorf("Items = %+v", out.Items)
	}
}

func TestDecoder_Unmarshal_Array(t *testing.T) {
	dec := NewDecoder()
	vals := url.Values{
		"a[0]": {"1"},
		"a[1]": {"2"},
		"a[2]": {"3"},
	}
	var out struct {
		A [3]int `form:"a"`
	}
	if err := dec.Unmarshal(vals, &out); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if out.A != [3]int{1, 2, 3} {
		t.Errorf("A = %v", out.A)
	}
}

func TestDecoder_Unmarshal_MapOfStruct(t *testing.T) {
	dec := NewDecoder()
	vals := url.Values{
		"m[key].name": {"n"},
	}
	var out struct {
		M map[string]struct {
			Name string `form:"name"`
		} `form:"m"`
	}
	if err := dec.Unmarshal(vals, &out); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if out.M["key"].Name != "n" {
		t.Errorf("M = %+v", out.M)
	}
}

// Regression: promoted fields from an embedded struct must be addressed by an
// index that is valid on the OUTER struct (embedded field's index prepended),
// not the embedded type's own index. Previously the value was silently dropped
// or landed in an unrelated sibling field.
type decodeEmbedCreds struct {
	Token string `form:"token,required"`
}

type decodeEmbedReq struct {
	decodeEmbedCreds
	Note string `form:"note"`
}

func TestDecoder_Unmarshal_EmbeddedPromotedField(t *testing.T) {
	dec := NewDecoder()
	var r decodeEmbedReq
	if err := dec.Unmarshal(url.Values{"token": {"secret-abc"}}, &r); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if r.Token != "secret-abc" {
		t.Errorf("Token = %q, want %q (Note=%q)", r.Token, "secret-abc", r.Note)
	}
	if r.Note != "" {
		t.Errorf("Note = %q, want empty; value leaked to a sibling field", r.Note)
	}
}

func TestDecoder_Unmarshal_EmbeddedPromotedFieldAndSibling(t *testing.T) {
	dec := NewDecoder()
	var r decodeEmbedReq
	if err := dec.Unmarshal(url.Values{"token": {"t"}, "note": {"n"}}, &r); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if r.Token != "t" || r.Note != "n" {
		t.Errorf("got Token=%q Note=%q, want %q %q", r.Token, r.Note, "t", "n")
	}
}

// Required must fire for an embedded field's promoted key that was never set.
func TestDecoder_Unmarshal_EmbeddedPromotedFieldRequired(t *testing.T) {
	dec := NewDecoder()
	var r decodeEmbedReq
	if err := dec.Unmarshal(url.Values{"note": {"x"}}, &r); err == nil {
		t.Error("expected missing-required error for embedded token")
	}
}

func TestDecoder_Unmarshal_EmbeddedPromotedFieldMultipart(t *testing.T) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if fw, err := mw.CreateFormField("token"); err != nil {
		t.Fatalf("create field: %v", err)
	} else if _, err := fw.Write([]byte("mp-secret")); err != nil {
		t.Fatalf("write field: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	var r decodeEmbedReq
	if err := Unmarshal(buf.Bytes(), mw.FormDataContentType(), &r); err != nil {
		t.Fatalf("unmarshal multipart error: %v", err)
	}
	if r.Token != "mp-secret" {
		t.Errorf("Token = %q, want %q", r.Token, "mp-secret")
	}
}

// Regression: a client-supplied huge index must not force a huge allocation.
func TestDecoder_Unmarshal_SliceIndexCapped(t *testing.T) {
	type s struct {
		Items []string `form:"items"`
	}

	dec := NewDecoder()
	var out s
	if err := dec.Unmarshal(url.Values{"items[100001]": {"x"}}, &out); err == nil {
		t.Fatal("expected error for index beyond max slice index")
	}
	if out.Items != nil {
		t.Errorf("Items should not have grown, got len=%d", len(out.Items))
	}

	// Within the cap still works.
	var ok s
	if err := dec.Unmarshal(url.Values{"items[5]": {"x"}}, &ok); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if len(ok.Items) != 6 || ok.Items[5] != "x" {
		t.Errorf("Items = %v", ok.Items)
	}

	// Negative indices are rejected, not a panic.
	var neg s
	if err := dec.Unmarshal(url.Values{"items[-1]": {"x"}}, &neg); err == nil {
		t.Error("expected error for negative index")
	}
}

func TestDecoder_Unmarshal_SliceIndexCustomCap(t *testing.T) {
	type s struct {
		Items []string `form:"items"`
	}
	dec := NewDecoder(WithMaxSliceIndex(10))
	var out s
	if err := dec.Unmarshal(url.Values{"items[10]": {"x"}}, &out); err == nil {
		t.Error("expected error at or above custom cap")
	}
	var ok s
	if err := dec.Unmarshal(url.Values{"items[9]": {"x"}}, &ok); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if len(ok.Items) != 10 {
		t.Errorf("Items len = %d, want 10", len(ok.Items))
	}
}

func TestDecoder_Unmarshal_SliceIndexCapDisabled(t *testing.T) {
	type s struct {
		Items []string `form:"items"`
	}
	dec := NewDecoder(WithMaxSliceIndex(0))
	var out s
	if err := dec.Unmarshal(url.Values{"items[200000]": {"x"}}, &out); err != nil {
		t.Fatalf("unmarshal error with cap disabled: %v", err)
	}
	if len(out.Items) != 200001 {
		t.Errorf("Items len = %d, want 200001", len(out.Items))
	}
}

// Regression: an out-of-range index on a fixed-size array must return an
// error, not panic.
type decodeFixedArr struct {
	Items [3]string `form:"items"`
}

func TestDecoder_Unmarshal_ArrayOutOfRange(t *testing.T) {
	defer func() {
		if rcv := recover(); rcv != nil {
			t.Fatalf("panicked on out-of-range array index: %v", rcv)
		}
	}()
	dec := NewDecoder()
	var a decodeFixedArr
	if err := dec.Unmarshal(url.Values{"items[10]": {"x"}}, &a); err == nil {
		t.Fatal("expected error for out-of-range array index")
	}
}

func TestDecoder_Unmarshal_ArrayWithinRange(t *testing.T) {
	dec := NewDecoder()
	var a decodeFixedArr
	if err := dec.Unmarshal(url.Values{"items[1]": {"x"}}, &a); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if a.Items[1] != "x" {
		t.Errorf("Items[1] = %q", a.Items[1])
	}
}

// Regression: a tag name shared by an embedded (promoted) field and an outer
// field, or by two sibling fields, previously decoded one field's payload into
// whichever field the index happened to register last — the other field was
// silently zeroed, with no error even under WithStrictUnmarshal.
type decodeInner struct {
	Name string `form:"name"`
}

type decodeCollideOuter struct {
	decodeInner
	Name string `form:"name"`
}

type decodeCollideSiblings struct {
	A string `form:"name"`
	B string `form:"name"`
}

func TestDecoder_Unmarshal_AmbiguousField(t *testing.T) {
	dec := NewDecoder()
	var out decodeCollideOuter
	err := dec.Unmarshal(url.Values{"name": {"inner-value"}}, &out)
	var de *DecodingError
	if !errors.As(err, &de) {
		t.Fatalf("expected DecodingError, got %v", err)
	}
	if de.Key != "name" {
		t.Errorf("Key = %q, want %q", de.Key, "name")
	}
	// Neither field may be written when the key is ambiguous.
	if out.Name != "" || out.decodeInner.Name != "" {
		t.Errorf("fields mutated despite ambiguous key: %+v", out)
	}
}

func TestDecoder_Unmarshal_AmbiguousFieldSiblings(t *testing.T) {
	dec := NewDecoder(WithStrictUnmarshal(true))
	var out decodeCollideSiblings
	err := dec.Unmarshal(url.Values{"name": {"x"}}, &out)
	var de *DecodingError
	if !errors.As(err, &de) {
		t.Fatalf("expected DecodingError, got %v", err)
	}
	if de.Key != "name" {
		t.Errorf("Key = %q, want %q", de.Key, "name")
	}
	if out.A != "" || out.B != "" {
		t.Errorf("fields mutated despite ambiguous key: %+v", out)
	}
}

func TestDecoder_Unmarshal_AmbiguousNestedField(t *testing.T) {
	type inner struct {
		V string `form:"v"`
	}
	type collide struct {
		X inner  `form:"x"`
		V string `form:"x"` // tag collides with the struct field's key
	}
	var out collide
	err := NewDecoder().Unmarshal(url.Values{"x.v": {"val"}}, &out)
	var de *DecodingError
	if !errors.As(err, &de) {
		t.Fatalf("expected DecodingError, got %v", err)
	}
	if de.Key != "x.v" {
		t.Errorf("Key = %q, want %q", de.Key, "x.v")
	}
	if strings.Contains(de.Err.Error(), "ambiguous") == false {
		t.Errorf("expected ambiguous-cause message, got %v", de.Err)
	}
}

// Regression: a scalar parse failure on a plain field (int overflow, bad bool)
// escaped as a bare error, unlike the same failure reached via a map/slice
// value. Every decode failure must satisfy errors.As(..., &DecodingError{}).
func TestDecoder_Unmarshal_ScalarErrorWrapped(t *testing.T) {
	type s struct {
		Int  int   `form:"int"`
		Bool bool  `form:"bool"`
		S    []int `form:"s"`
	}
	dec := NewDecoder()

	for _, key := range []string{"int", "bool", "s[0]"} {
		var out s
		var raw string
		switch key {
		case "int":
			raw = "999999999999999999999"
		case "bool":
			raw = "notabool"
		case "s[0]":
			raw = "999999999999999999999"
		}
		err := dec.Unmarshal(url.Values{key: {raw}}, &out)
		var de *DecodingError
		if !errors.As(err, &de) {
			t.Errorf("%s: expected DecodingError, got %v", key, err)
			continue
		}
		if de.Err == nil {
			t.Errorf("%s: DecodingError has nil cause", key)
		}
	}
}

// Regression: zero-valued time.Time was unconditionally omitted from marshal
// output, unlike every other type (int → "0", string → ""), contradicting the
// documented default that zero values are emitted. It now formats normally.
func TestEncoder_Marshal_ZeroTimeEmitted(t *testing.T) {
	type s struct {
		When time.Time `form:"when"`
	}
	vals, err := NewEncoder().Marshal(s{})
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	if vals.Get("when") != "0001-01-01T00:00:00Z" {
		t.Errorf("when = %q, want %q", vals.Get("when"), "0001-01-01T00:00:00Z")
	}
}

func TestEncoder_Marshal_ZeroTimeOmittedWithOmitEmpty(t *testing.T) {
	type s struct {
		When time.Time `form:"when"`
	}
	vals, err := NewEncoder(WithZeroEmpty(true)).Marshal(s{})
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	if vals.Has("when") {
		t.Errorf("zero time should be omitted under WithZeroEmpty, got %v", vals)
	}
}
