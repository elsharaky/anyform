package anyform

import (
	"net/url"
	"testing"
)

// Benchmark struct matching common real-world usage.
type benchUser struct {
	ID       int               `form:"id"`
	Name     string            `form:"name"`
	Email    string            `json:"user_email"`
	Enabled  bool              `form:"enabled"`
	Roles    []string          `form:"roles"`
	Metadata map[string]string `form:"meta"`
	Profile  benchProfile      `form:"profile"`
	Score    float64           `json:"score"`
	Age      int               `form:"age"`
	Bio      string            `form:"bio"`
	Website  string            `form:"website"`
	Phone    string            `form:"phone"`
	Company  string            `json:"company"`
	Title    string            `json:"title"`
	Extra    string            `form:"extra"`
	Extra2   string            `form:"extra2"`
}

type benchProfile struct {
	FirstName string `form:"first"`
	LastName  string `form:"last"`
	Country   string `json:"country"`
}

var benchInput = benchUser{
	ID:      42,
	Name:    "Alice",
	Email:   "alice@example.com",
	Enabled: true,
	Roles:   []string{"admin", "editor", "viewer"},
	Metadata: map[string]string{
		"theme": "dark",
		"tz":    "UTC",
		"lang":  "en",
	},
	Profile: benchProfile{FirstName: "A", LastName: "B", Country: "US"},
	Score:   9.75,
	Age:     30,
	Bio:     "Hello world, this is a longer bio field used in benchmarks",
	Website: "https://example.com",
	Phone:   "+1-555-0100",
	Company: "ACME Corp",
	Title:   "Engineer",
	Extra:   "x",
	Extra2:  "y",
}

var benchVals = func() url.Values {
	enc := NewEncoder()
	v, _ := enc.Marshal(benchInput)
	return v
}()

func BenchmarkMarshal_Small(b *testing.B) {
	enc := NewEncoder()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := enc.Marshal(struct {
			Name string `form:"name"`
			Age  int    `form:"age"`
		}{"x", 1}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMarshal_Medium(b *testing.B) {
	enc := NewEncoder()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := enc.Marshal(benchInput); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMarshal_Nested(b *testing.B) {
	enc := NewEncoder()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := enc.Marshal(benchInput); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkUnmarshal_Medium(b *testing.B) {
	dec := NewDecoder()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var out benchUser
		if err := dec.Unmarshal(benchVals, &out); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkUnmarshal_SliceOfStruct(b *testing.B) {
	dec := NewDecoder()
	vals := url.Values{
		"items[0].name": {"a"},
		"items[1].name": {"b"},
		"items[2].name": {"c"},
	}
	type items struct {
		Items []struct {
			Name string `form:"name"`
		} `form:"items"`
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var out items
		if err := dec.Unmarshal(vals, &out); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkUnmarshal_Map(b *testing.B) {
	dec := NewDecoder()
	vals := url.Values{
		"attr[x]": {"1"},
		"attr[y]": {"2"},
		"attr[z]": {"3"},
	}
	type m struct {
		Attr map[string]string `form:"attr"`
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var out m
		if err := dec.Unmarshal(vals, &out); err != nil {
			b.Fatal(err)
		}
	}
}
