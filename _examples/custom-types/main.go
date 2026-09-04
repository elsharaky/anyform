// Command custom-types demonstrates registering a custom Converter, custom tag
// priority, and built-in type support.
package main

import (
	"fmt"
	"net/url"
	"reflect"
	"time"

	"github.com/elsharaky/anyform"
)

// Status is a custom integer type with a string converter.
type Status int

const (
	Pending Status = iota
	Active
	Blocked
)

type statusConverter struct{}

func (statusConverter) Marshal(v reflect.Value) (string, error) {
	switch Status(v.Int()) {
	case Pending:
		return "pending", nil
	case Active:
		return "active", nil
	case Blocked:
		return "blocked", nil
	}
	return "unknown", nil
}

func (statusConverter) Unmarshal(s string, f reflect.Value) error {
	switch s {
	case "pending":
		f.SetInt(int64(Pending))
	case "active":
		f.SetInt(int64(Active))
	case "blocked":
		f.SetInt(int64(Blocked))
	}
	return nil
}

type Event struct {
	Title    string        `form:"title" json:"title"`
	When     time.Time     `form:"when" json:"when"`
	Status   Status        `form:"status" json:"status"`
	Duration time.Duration `form:"duration" json:"duration"`
}

func main() {
	enc := anyform.NewEncoder(
		anyform.WithCustomConverter(reflect.TypeOf(Status(0)), statusConverter{}),
		anyform.WithTimeLayout(time.RFC3339),
	)
	in := Event{
		Title:    "Launch",
		When:     time.Date(2024, 5, 1, 12, 0, 0, 0, time.UTC),
		Status:   Active,
		Duration: 45 * time.Minute,
	}
	vals, err := enc.Marshal(in)
	if err != nil {
		panic(err)
	}
	fmt.Println("Marshalled:")
	fmt.Println(vals.Encode())

	dec := anyform.NewDecoder(
		anyform.WithCustomConverter(reflect.TypeOf(Status(0)), statusConverter{}),
	)
	var out Event
	src := url.Values{
		"title":    {"Demo"},
		"when":     {"2024-06-01T09:30:00Z"},
		"status":   {"blocked"},
		"duration": {"15m"},
	}
	if err := dec.Unmarshal(src, &out); err != nil {
		panic(err)
	}
	fmt.Printf("Unmarshalled: title=%s status=%d duration=%s\n", out.Title, out.Status, out.Duration)
}
