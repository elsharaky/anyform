package anyform

import (
	"reflect"
	"testing"
)

type testMarshalTags struct {
	Name       string `form:"user_name"`
	Email      string `json:"user_email"`
	Age        int    `xml:"age_xml" json:"age"`
	Skipped    string `form:"-"`
	Ignored    string `json:"-"`
	NoTag      string
	Protobuf   string `protobuf:"bytes,5,opt,name=proto_field"`
	Required   string `form:"required_field,required"`
	OmitEmpty  string `form:"omit_me,omitempty"`
	DefaultVal string `form:"default_field,default:hello"`
}

type testPriorityOrder struct {
	A string `form:"a_form" json:"a_json" xml:"a_xml" protobuf:"a_proto"`
	B string `json:"b_json" protobuf:"b_proto"`
	C string `xml:"c_xml"`
	D string
}

func TestTagResolver_marshalFieldName(t *testing.T) {
	r := newTagResolver()

	tests := []struct {
		field    string
		expected string
	}{
		{"Name", "user_name"},       // form tag wins
		{"Email", "user_email"},     // json tag (no form tag)
		{"Age", "age"},              // json tag (form > json > xml)
		{"Skipped", ""},             // skipped via form:"-"
		{"Ignored", ""},             // json tag is "-" (first existing tag) → skipped
		{"NoTag", "NoTag"},          // fallback to Go name
		{"Protobuf", "proto_field"}, // protobuf tag
	}

	sf := reflect.TypeOf(testMarshalTags{})

	for _, tt := range tests {
		t.Run(tt.field, func(t *testing.T) {
			field, ok := sf.FieldByName(tt.field)
			if !ok {
				t.Fatalf("field %s not found", tt.field)
			}
			got, skip := r.marshalFieldName(field)
			if tt.expected == "" {
				if !skip {
					t.Errorf("expected skip=true")
				}
				return
			}
			if skip {
				t.Fatalf("unexpected skip for %s", tt.field)
			}
			if got != tt.expected {
				t.Errorf("got %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestTagResolver_marshalFieldOptions(t *testing.T) {
	r := newTagResolver()
	sf := reflect.TypeOf(testMarshalTags{})

	field, _ := sf.FieldByName("Required")
	opts := r.marshalFieldOptions(field)
	if !opts.Required {
		t.Error("expected Required=true")
	}
	if opts.Name != "required_field" {
		t.Errorf("got name %q, want %q", opts.Name, "required_field")
	}

	field, _ = sf.FieldByName("OmitEmpty")
	opts = r.marshalFieldOptions(field)
	if !opts.OmitEmpty {
		t.Error("expected OmitEmpty=true")
	}
	if opts.Name != "omit_me" {
		t.Errorf("got name %q, want %q", opts.Name, "omit_me")
	}

	field, _ = sf.FieldByName("DefaultVal")
	opts = r.marshalFieldOptions(field)
	if !opts.HasDefault {
		t.Error("expected HasDefault=true")
	}
	if opts.Default != "hello" {
		t.Errorf("got default %q, want %q", opts.Default, "hello")
	}
}

func TestTagResolver_PriorityOrder(t *testing.T) {
	tests := []struct {
		name     string
		priority []string
		field    string
		expected string
	}{
		{"form first", []string{"form", "json", "xml", "protobuf"}, "A", "a_form"},
		{"json first", []string{"json", "form", "xml", "protobuf"}, "A", "a_json"},
		{"xml first", []string{"xml", "json", "form", "protobuf"}, "A", "a_xml"},
		{"protobuf first", []string{"protobuf", "json", "xml"}, "A", "a_proto"},
		{"json only", []string{"json"}, "B", "b_json"},
		{"protobuf only", []string{"protobuf"}, "B", "b_proto"},
		{"missing tag", []string{"form"}, "B", "B"},    // fallback to Go name
		{"no tags at all", []string{"form"}, "D", "D"}, // fallback to Go name
	}

	sf := reflect.TypeOf(testPriorityOrder{})

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newTagResolver(tt.priority...)
			field, ok := sf.FieldByName(tt.field)
			if !ok {
				t.Fatalf("field %s not found", tt.field)
			}
			got, _ := r.marshalFieldName(field)
			if got != tt.expected {
				t.Errorf("got %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestTagResolver_unmarshalFieldName(t *testing.T) {
	r := newTagResolver()
	tp := reflect.TypeOf(testMarshalTags{})

	tests := []struct {
		key      string
		wantName string
		wantOk   bool
	}{
		{"user_name", "Name", true},       // matches form tag
		{"user_email", "Email", true},     // matches json tag
		{"age_xml", "Age", true},          // matches xml tag
		{"age", "Age", true},              // matches json tag
		{"proto_field", "Protobuf", true}, // matches protobuf tag
		{"NoTag", "NoTag", true},          // matches Go name
		{"nonexistent", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			field, ok := r.unmarshalFieldName(tp, tt.key)
			if ok != tt.wantOk {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOk)
			}
			if tt.wantOk && field.Name != tt.wantName {
				t.Errorf("got field %q, want %q", field.Name, tt.wantName)
			}
		})
	}
}

func TestTagResolver_unmarshalFieldName_Skipped(t *testing.T) {
	r := newTagResolver()
	tp := reflect.TypeOf(testMarshalTags{})

	// Skipped fields should not be found by their tag name
	_, ok := r.unmarshalFieldName(tp, "-")
	if ok {
		t.Error("expected skipped field not to be found")
	}
}

func TestTagResolver_buildUnmarshalIndex(t *testing.T) {
	r := newTagResolver()
	tp := reflect.TypeOf(testMarshalTags{})
	idx := r.buildUnmarshalIndex(tp)

	expectedKeys := []string{
		"user_name", "Name",
		"user_email", "Email",
		"age_xml", "age", "Age",
		"proto_field", "Protobuf",
		"required_field", "Required",
		"omit_me", "OmitEmpty",
		"default_field", "DefaultVal",
		"NoTag",
	}

	for _, key := range expectedKeys {
		if _, ok := idx[key]; !ok {
			t.Errorf("key %q not found in unmarshal index", key)
		}
	}

	// Skipped fields should NOT be in the index
	if _, ok := idx["-"]; ok {
		t.Error("skipped field should not be in index")
	}
	// Ignored field (json:"-") has its first existing tag as "-", so it is fully
	// excluded from the index (neither its json value nor its Go name).
	if _, ok := idx["Ignored"]; ok {
		t.Error("field ignored via first-existing-tag '-' should not be in index")
	}
}

func TestParseTagOptions(t *testing.T) {
	tests := []struct {
		tag  string
		want tagOptions
	}{
		{
			"name",
			tagOptions{Name: "name"},
		},
		{
			"-",
			tagOptions{Name: "-", Skip: true},
		},
		{
			"name,omitempty",
			tagOptions{Name: "name", OmitEmpty: true},
		},
		{
			"name,required",
			tagOptions{Name: "name", Required: true},
		},
		{
			"name,default:foo",
			tagOptions{Name: "name", Default: "foo", HasDefault: true},
		},
		{
			"name,omitempty,required,default:bar",
			tagOptions{Name: "name", OmitEmpty: true, Required: true, Default: "bar", HasDefault: true},
		},
		{
			"",
			tagOptions{Name: ""},
		},
	}

	for _, tt := range tests {
		t.Run(tt.tag, func(t *testing.T) {
			got := parseTagOptions(tt.tag)
			if got != tt.want {
				t.Errorf("parseTagOptions(%q) = %+v, want %+v", tt.tag, got, tt.want)
			}
		})
	}
}

func TestTagResolver_Priority(t *testing.T) {
	r := newTagResolver("a", "b", "c")
	p := r.priorities()
	if len(p) != 3 || p[0] != "a" || p[1] != "b" || p[2] != "c" {
		t.Errorf("unexpected priority: %v", p)
	}
	// Ensure it returns a copy
	p[0] = "x"
	if r.priorities()[0] != "a" {
		t.Error("Priority() should return a copy")
	}
}

func TestDefaultTagResolver(t *testing.T) {
	r := newTagResolver()
	p := r.priorities()
	if len(p) != 4 || p[0] != "form" || p[1] != "json" || p[2] != "xml" || p[3] != "protobuf" {
		t.Errorf("unexpected default priority: %v", p)
	}
}

type testEmbeddedStruct struct {
	testMarshalTags
	Extra string `form:"extra"`
}

func TestTagResolver_EmbeddedStruct(t *testing.T) {
	r := newTagResolver()
	tp := reflect.TypeOf(testEmbeddedStruct{})

	// Should find fields from embedded struct
	field, ok := r.unmarshalFieldName(tp, "user_name")
	if !ok || field.Name != "Name" {
		t.Errorf("expected to find Name via form tag in embedded struct")
	}

	// Should find direct field
	field, ok = r.unmarshalFieldName(tp, "extra")
	if !ok || field.Name != "Extra" {
		t.Errorf("expected to find Extra via form tag")
	}
}

func TestUnmarshalFieldTagValue(t *testing.T) {
	sf := reflect.TypeOf(testMarshalTags{}).Field(0) // Name with form:"user_name"

	name, ok := unmarshalFieldTagValue(sf, "form")
	if !ok || name != "user_name" {
		t.Errorf("got %q, %v; want user_name, true", name, ok)
	}

	_, ok = unmarshalFieldTagValue(sf, "nonexistent")
	if ok {
		t.Error("expected false for nonexistent tag")
	}
}

func TestUnmarshalFieldTagOptions(t *testing.T) {
	sf := reflect.TypeOf(testMarshalTags{}).Field(7) // Required

	opts, ok := unmarshalFieldTagOptions(sf, "form")
	if !ok {
		t.Fatal("expected true")
	}
	if !opts.Required {
		t.Error("expected Required=true")
	}
	if opts.Name != "required_field" {
		t.Errorf("got name %q, want %q", opts.Name, "required_field")
	}
}
