package anyform

import (
	"net/url"
	"reflect"
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
