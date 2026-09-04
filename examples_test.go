package anyform_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"

	"github.com/elsharaky/anyform"
)

func ExampleEncoder_Marshal() {
	type User struct {
		Name  string `form:"name"`
		Email string `form:"email"`
		Age   int    `form:"age"`
	}

	enc := anyform.NewEncoder()
	vals, err := enc.Marshal(User{
		Name:  "Alice",
		Email: "alice@example.com",
		Age:   30,
	})
	if err != nil {
		fmt.Println("error:", err)
		return
	}

	fmt.Println(vals.Get("name"))
	fmt.Println(vals.Get("email"))
	fmt.Println(vals.Get("age"))
	// Output:
	// Alice
	// alice@example.com
	// 30
}

func ExampleDecoder_Unmarshal() {
	type User struct {
		Name  string `form:"name"`
		Email string `form:"email"`
		Age   int    `form:"age"`
	}

	vals := url.Values{
		"name":  {"Bob"},
		"email": {"bob@example.com"},
		"age":   {"25"},
	}

	var user User
	dec := anyform.NewDecoder()
	if err := dec.Unmarshal(vals, &user); err != nil {
		fmt.Println("error:", err)
		return
	}

	fmt.Printf("%s <%s> is %d years old\n", user.Name, user.Email, user.Age)
	// Output: Bob <bob@example.com> is 25 years old
}

func Example_tagPriority() {
	// Fields can declare multiple tags. During marshalling, the first tag in the
	// priority order that exists on the field is used. The default order is
	// form > json > xml > protobuf.
	type Product struct {
		ID   int    `json:"product_id"`
		Name string `form:"product_name" json:"product_name"`
		Slug string `xml:"slug" json:"slug"`
		Note string
	}

	enc := anyform.NewEncoder()
	vals, _ := enc.Marshal(Product{
		ID:   7,
		Name: "Widget",
		Slug: "widget-7",
		Note: "hello",
	})

	fmt.Println(vals.Get("product_id"))   // json tag (no form tag) -> 7
	fmt.Println(vals.Get("product_name")) // form tag wins -> Widget
	fmt.Println(vals.Get("slug"))         // json tag wins over xml -> widget-7
	fmt.Println(vals.Get("Note"))         // falls back to Go field name -> hello
	// Output:
	// 7
	// Widget
	// widget-7
	// hello
}

func Example_customTagPriority() {
	type Payload struct {
		ID   int    `json:"json_id" protobuf:"bytes,1,opt,name=proto_id"`
		Name string `form:"form_name" json:"json_name"`
	}

	// Prioritize json over form.
	enc := anyform.NewEncoder(anyform.WithTagPriority("json", "form", "protobuf"))
	vals, _ := enc.Marshal(Payload{ID: 3, Name: "n"})

	fmt.Println(vals.Get("json_id"))
	fmt.Println(vals.Get("json_name"))
	// Output:
	// 3
	// n
}

func Example_fileUpload() {
	type Upload struct {
		Title  string       `form:"title"`
		Avatar anyform.File `form:"avatar"`
	}

	// client marshals into multipart
	enc := anyform.NewEncoder()
	body, contentType, err := enc.MarshalMultipart(Upload{
		Title:  "My Avatar",
		Avatar: anyform.File{Content: []byte("image-bytes"), ContentType: "image/png", Filename: "me.png"},
	})
	if err != nil {
		fmt.Println("error:", err)
		return
	}

	// server decodes from the request
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", contentType)
	_ = req.ParseMultipartForm(1 << 20)

	var upload Upload
	dec := anyform.NewDecoder()
	if err := dec.UnmarshalMultipart(req, &upload); err != nil {
		fmt.Println("error:", err)
		return
	}

	fmt.Println(upload.Title)
	fmt.Println(string(upload.Avatar.Content))
	fmt.Println(upload.Avatar.Filename)
	// Output:
	// My Avatar
	// image-bytes
	// me.png
}

func Example_customConverter() {
	// Convert a custom type to/from a string using a Converter.
	enc := anyform.NewEncoder(anyform.WithCustomConverter(reflect.TypeOf(exampleStatus(0)), exampleStatusConverter{}))
	vals, err := enc.Marshal(exampleAccount{Status: exampleStatusActive})
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println(vals.Get("status"))
	// Output:
	// active
}

type exampleStatus int

const (
	exampleStatusPending exampleStatus = iota
	exampleStatusActive
	exampleStatusBlocked
)

type exampleAccount struct {
	Status exampleStatus `form:"status"`
}

type exampleStatusConverter struct{}

func (exampleStatusConverter) Marshal(v reflect.Value) (string, error) {
	switch exampleStatus(v.Int()) {
	case exampleStatusPending:
		return "pending", nil
	case exampleStatusActive:
		return "active", nil
	case exampleStatusBlocked:
		return "blocked", nil
	}
	return "", nil
}

func (exampleStatusConverter) Unmarshal(s string, f reflect.Value) error {
	switch s {
	case "pending":
		f.SetInt(int64(exampleStatusPending))
	case "active":
		f.SetInt(int64(exampleStatusActive))
	case "blocked":
		f.SetInt(int64(exampleStatusBlocked))
	}
	return nil
}

func ExampleMarshal() {
	// The unified Marshal builds a body and its Content-Type in one step.
	type User struct {
		Name string `form:"name"`
		Age  int    `form:"age"`
	}

	body, ct, err := anyform.Marshal(User{Name: "Alice", Age: 30})
	if err != nil {
		fmt.Println("error:", err)
		return
	}

	fmt.Println(ct)
	fmt.Println(string(body))
	// Output:
	// application/x-www-form-urlencoded
	// age=30&name=Alice
}

func ExampleUnmarshal() {
	// The unified Unmarshal decodes a body back into a struct, ignoring the
	// format plumbing. It detects multipart vs url-encoded from the Content-Type.
	type User struct {
		Name string `form:"name"`
		Age  int    `form:"age"`
	}

	body, ct, _ := anyform.Marshal(User{Name: "Bob", Age: 25})

	var user User
	if err := anyform.Unmarshal(body, ct, &user); err != nil {
		fmt.Println("error:", err)
		return
	}

	fmt.Printf("%s is %d\n", user.Name, user.Age)
	// Output: Bob is 25
}

func Example_unifiedFileUpload() {
	// The unified API auto-detects File fields and produces/consumes multipart,
	// with no net/http dependency on either side.
	type Upload struct {
		Title  string         `form:"title"`
		Avatar anyform.File   `form:"avatar"`
		Docs   []anyform.File `form:"documents"`
	}

	req := Upload{
		Title:  "Quarterly Report",
		Avatar: anyform.File{Filename: "report.pdf", ContentType: "application/pdf", Content: []byte("%PDF")},
		Docs: []anyform.File{
			{Filename: "notes.txt", ContentType: "text/plain", Content: []byte("notes")},
		},
	}

	body, ct, err := anyform.Marshal(req)
	if err != nil {
		fmt.Println("error:", err)
		return
	}

	var resp Upload
	if err := anyform.Unmarshal(body, ct, &resp); err != nil {
		fmt.Println("error:", err)
		return
	}

	fmt.Println(resp.Title)
	fmt.Println(resp.Avatar.Filename, string(resp.Avatar.Content))
	fmt.Println(len(resp.Docs), resp.Docs[0].Filename)
	// Output:
	// Quarterly Report
	// report.pdf %PDF
	// 1 notes.txt
}

func Example_nestedSlicesAndMaps() {
	// Nested structs (dot), slices (index), maps (key), and repetition all work.
	type Address struct {
		City string `form:"city"`
		ZIP  string `form:"zip"`
	}
	type Order struct {
		ID       int               `form:"id"`
		ShipTo   Address           `form:"ship_to"`
		Lines    []string          `form:"line"`
		Discount map[string]string `form:"discount"`
	}

	order := Order{
		ID:       42,
		ShipTo:   Address{City: "Lyon", ZIP: "69001"},
		Lines:    []string{"A", "B"},
		Discount: map[string]string{"code": "SAVE10"},
	}

	body, ct, err := anyform.Marshal(order)
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println(string(body))

	var got Order
	if err := anyform.Unmarshal(body, ct, &got); err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Printf("%d %s %s %v %v\n", got.ID, got.ShipTo.City, got.ShipTo.ZIP, got.Lines, got.Discount)
	// Output:
	// discount%5Bcode%5D=SAVE10&id=42&line%5B0%5D=A&line%5B1%5D=B&ship_to.city=Lyon&ship_to.zip=69001
	// 42 Lyon 69001 [A B] map[code:SAVE10]
}

func Example_defaultAndRequiredTags() {
	// default: populates absent fields; required: errors with ErrMissingRequired.
	type Config struct {
		Region string `form:"region,default:us-east"`
		Token  string `form:"token,required"`
	}

	dec := anyform.NewDecoder()

	// token provided, region absent -> default is applied.
	var cfg Config
	if err := dec.Unmarshal(url.Values{"token": {"abc"}}, &cfg); err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Printf("region=%s token=%s\n", cfg.Region, cfg.Token)

	// token absent -> required error.
	var missing Config
	err := dec.Unmarshal(url.Values{"region": {"eu"}}, &missing)
	fmt.Println(err)
	// Output:
	// region=us-east token=abc
	// anyform: decoding (key "token"): anyform: missing required field
}
