# goform — Developer Tutorial

A practical, step-by-step guide to using `goform` in your Go application. By
the end you'll be able to marshal structs into request bodies and unmarshal
request bodies back into structs, including file uploads, nested types, and
custom converters.

If you're implementing or maintaining `goform` itself, see
[MAINTAINER.md](MAINTAINER.md).

---

## 1. What goform does

`goform` serializes Go structs to and from **HTML form data** in two flavors:

- **`application/x-www-form-urlencoded`** — flat `key=value` strings.
- **`multipart/form-data`** — used whenever the data contains file uploads.

The library is **zero-dependency** and **HTTP-agnostic** (it never imports
`net/http` in its core). You give it a `[]byte` body plus a `Content-Type`, and
it figures out the format for you — or you produce exactly that pair.

---

## 2. Installation

```bash
go get github.com/elsharaky/goform
```

Requires **Go 1.22+** (what `go.mod` declares). CI builds and tests on Go 1.27.

---

## 3. The quick start

The entire library surfaces as two functions:

```go
import "github.com/elsharaky/goform"

type User struct {
    Name  string `form:"name"`
    Email string `form:"email"`
    Age   int    `form:"age"`
}

// Marshal: struct -> (body bytes, Content-Type)
body, ct, err := goform.Marshal(User{Name: "Alice", Email: "alice@example.com", Age: 30})
// body == "age=30&email=alice%40example.com&name=Alice"
// ct   == "application/x-www-form-urlencoded"

// Unmarshal: (body bytes, Content-Type) -> struct
var user User
err := goform.Unmarshal(body, ct, &user)
```

That's the whole mental model. `Marshal` returns the exact pair you'd put in a
request or response; `Unmarshal` consumes the exact pair you'd receive.

### Using it with net/http

```go
// Client side
body, ct, _ := goform.Marshal(payload)
resp, _ := http.Post(url, ct, bytes.NewReader(body))

// Server side
readBody, _ := io.ReadAll(r.Body)
var payload Payload
if err := goform.Unmarshal(readBody, r.Header.Get("Content-Type"), &payload); err != nil {
    http.Error(w, err.Error(), http.StatusBadRequest)
    return
}
```

No manual `ParseMultipartForm`, no `url.ParseQuery`, no boundary handling.

---

## 4. How tags work

Fields are resolved by priority. The default order is:

```
form > json > xml > protobuf
```

The **first tag present** on a field wins during marshalling; if none exists,
the exported Go field name is used.

```go
type Product struct {
    ID   int    `json:"product_id"`                        // -> product_id
    Name string `form:"product_name" json:"product_name"`  // -> product_name (form wins)
    Slug string `xml:"slug" json:"slug"`                   // -> slug (json wins)
    Note string                                            // -> Note (Go field name)
}
```

- **Marshalling** always uses the highest-priority existing tag.
- **Unmarshalling** accepts *any* tag name as the submitted key, so a client
  sending `product_id` (json) or `product_name` (form) both work.

### Skipping fields

A field whose **first existing tag** is `-` is skipped:

```go
type T struct {
    Secret string `json:"-"`          // skipped (no higher tag)
    Public string `form:"pub" json:"-"` // NOT skipped: form tag participates
}
```

---

## 5. Options

Both `Marshal`/`Unmarshal` and `NewEncoder`/`NewDecoder` accept functional
options. Options customize a *single* call in the unified API, or a reusable
instance in the Encoder/Decoder API.

| Option | Default | Effect |
|---|---|---|
| `WithTagPriority(...tags)` | `form>json>xml>protobuf` | Change marshal tag order |
| `WithMaxDepth(n)` | `32` | Max nesting depth → `ErrMaxDepthExceeded` |
| `WithMaxSliceIndex(n)` | `100000` | Max slice growth from a client `[i]` index (0 = unlimited) → `DecodingError` |
| `WithTimeLayout(layout)` | `time.RFC3339` | `time.Time` format string |
| `WithZeroEmpty(bool)` | `false` | Omit all zero-valued fields on marshal |
| `WithCustomConverter(type, conv)` | built-ins | Override a type's (un)marshal |
| `WithTextMarshalerSupport(bool)` | `true` | Auto `TextMarshaler`/`TextUnmarshaler` |
| `WithStrictUnmarshal(bool)` | `false` | Error on unknown submitted keys |
| `WithMaxBodySize(bytes)` | unlimited | Max body size → `ErrBodyTooLarge` |
| `WithMaxFileSize(bytes)` | unlimited | Max per-file size → `ErrFileTooLarge` |

Example:

```go
body, ct, _ := goform.Marshal(
    event,
    goform.WithTimeLayout("2006-01-02"),
    goform.WithZeroEmpty(true),
)
```

### Reusable Encoder / Decoder

When you configure the same options everywhere (a typical server), build a
shared instance instead. They are safe for concurrent use.

```go
dec := goform.NewDecoder(
    goform.WithStrictUnmarshal(true),
    goform.WithTimeLayout("2006-01-02"),
)
// reuse dec across requests via dec.Unmarshal(vals, &v)
```

---

## 6. Tag options on fields

```go
type Config struct {
    Nickname string `form:"nick,omitempty"`         // omits empty values on marshal
    Token    string `form:"token,required"`         // errors if absent on unmarshal
    Region   string `form:"region,default:us-east"` // fills an absent value
}
```

- `,omitempty` — skip empty values when marshalling.
- `,required` — return `ErrMissingRequired` when the field is absent on
  unmarshal. Check with `errors.Is(err, goform.ErrMissingRequired)`.
- `,default:v` — populate an absent **scalar** field with `v` on unmarshal.

A field counts as provided when *any* submitted key resolves to it, so a
default never overwrites a value the client actually sent — including via a
nested key (`ship_to.city`), an indexed key, or an alternate tag name
(`json:"reg"` addressing a `form:"region"` field).

Run `go run ./_examples/validation` for a working version of all three.

---

## 7. Supported Go types

`goform` handles essentially everything:

- **Scalars** — `string`, `bool`, all `int`/`uint`/`float`/`complex` widths,
  parsed at the field's own bit size (so an out-of-range number is an error, not
  a silent `+Inf`).
- **Named types** — `type MyInt int`, `type Status string`.
- **Pointers** — `*T` (nil omitted on marshal, allocated on unmarshal).
- **Slices / arrays** — indexed keys (`items[0]`).
- **`[]byte`** — one opaque value (`data=hello`), not one key per byte.
- **Maps** — bracket-named keys.
- **Structs** — nested and embedded structs.
- **Time** — `time.Time`, `time.Duration`.
- **Networking** — `net.IP`, `url.URL`.
- **Text interfaces** — any `encoding.TextMarshaler`/`TextUnmarshaler`.
- **Files** — `File`, `*File`, and `[]File`.

---

## 8. Nested structs, slices, and maps

Keys follow a predictable format:

| Key | Meaning |
|---|---|
| `name` | field |
| `address.city` | nested field |
| `items[0]` | slice index |
| `items[0].name` | index then field |
| `attr[key]` | map key |
| `matrix[0][1]` | nested indexes |

```go
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

body, ct, _ := goform.Marshal(Order{
    ID:       42,
    ShipTo:   Address{City: "Lyon", ZIP: "69001"},
    Lines:    []string{"A", "B"},
    Discount: map[string]string{"code": "SAVE10"},
})
// body: discount%5Bcode%5D=SAVE10&id=42&line%5B0%5D=A&line%5B1%5D=B&ship_to.city=Lyon&ship_to.zip=69001

var got Order
goform.Unmarshal(body, ct, &got) // reconstructs everything
```

> Repeating a scalar key (`k=a&k=b`) yields a `[]string` / slice on unmarshal.

---

## 9. File uploads

`goform.File` decouples file handling from `net/http`:

```go
type File struct {
    Content     []byte // raw bytes
    ContentType string // MIME type
    Filename    string // original name
}
```

Define a field as `File`, `*File`, or `[]File`. `Marshal` **auto-detects** the
`File` fields — at any nesting, including inside slices and maps — and switches
to multipart automatically.

```go
type Upload struct {
    Title  string         `form:"title"`
    Avatar goform.File   `form:"avatar"`
    Docs   []goform.File `form:"documents"`
}

// Client
body, ct, err := goform.Marshal(Upload{
    Title:  "Report",
    Avatar: goform.File{Filename: "report.pdf", Content: []byte("%PDF"), ContentType: "application/pdf"},
    Docs:   []goform.File{{Filename: "n.txt", Content: []byte("n"), ContentType: "text/plain"}},
})

// Server (HTTP-agnostic decode)
var up Upload
err := goform.Unmarshal(body, ct, &up)
fmt.Println(up.Avatar.Filename, string(up.Avatar.Content))
```

> **Note:** file parts should carry a `Filename` for reliable multipart
> detection. A part with an empty filename is classified as a value field by the
> multipart parser. A part that *is* present but carries zero bytes still binds
> — the `File` keeps its `Filename` with empty `Content`. The encoder mirrors
> this: a `File` with an empty filename is skipped, so anything you marshal
> round-trips.

> **Security note:** the library reads each file fully into memory and, by
> default, imposes **no size limit**. For untrusted uploads, cap both the
> request (`http.MaxBytesReader`) and per-file size
> (`WithMaxFileSize` → `ErrFileTooLarge`). An oversized file part is rejected
> using its declared size **before** the content is read into memory:

```go
var up Upload
err := goform.Unmarshal(body, ct, &up,
    goform.WithMaxBodySize(10 << 20), // whole body ≤ 10 MiB
    goform.WithMaxFileSize(5 << 20),  // each file ≤ 5 MiB
)
if errors.Is(err, goform.ErrFileTooLarge) {
    // respond 413 Payload Too Large
}
```

### Working from an `http.Request` directly

Two helpers read files out without a full decode:

```go
// All files under one field name (nil, not an error, when there are none).
files, err := goform.FilesFromRequest(req, "documents") // []File

// A single part, after the request has been parsed with ParseMultipartForm.
file, err := goform.FileFromHeader(req.MultipartForm.File["avatar"][0])
```

Both read the content fully into memory and sniff a `ContentType` when the
part does not declare one.

### Bounding client-supplied slice indices

`WithMaxBodySize` does not bound `items[1000000]=x`: the body is tiny, but the
allocation is not. `WithMaxSliceIndex` caps how far a `[i]` key may grow a slice
(default `100000`; `0` disables it), and the offending key fails with a
`*DecodingError`:

```go
dec := goform.NewDecoder(goform.WithMaxSliceIndex(1000))
```

---

## 10. Custom converters

For exotic types, implement the `Converter` interface and register it:

```go
type Converter interface {
    Marshal(value reflect.Value) (string, error)
    Unmarshal(value string, field reflect.Value) error
}
```

```go
type Status int // wants "active"/"blocked" strings

type statusConverter struct{}
func (statusConverter) Marshal(v reflect.Value) (string, error) {
    switch Status(v.Int()) {
    case Active: return "active", nil
    case Blocked: return "blocked", nil
    }
    return "", nil
}
func (statusConverter) Unmarshal(s string, f reflect.Value) error {
    switch s {
    case "active": f.SetInt(int64(Active))
    case "blocked": f.SetInt(int64(Blocked))
    }
    return nil
}

enc := goform.NewEncoder(goform.WithCustomConverter(reflect.TypeOf(Status(0)), statusConverter{}))
vals, _ := enc.Marshal(acct) // status=active
```

Built-in converters already handle `time.Duration`, `net.IP`, `url.URL`; and
`time.Time` is handled via the layout option.

---

## 11. Error handling

Two wrapper types carry context, plus sentinels for `errors.Is`:

```go
type EncodingError struct {
    FieldPath string
    Err       error
}
type DecodingError struct {
    FieldPath string
    Key       string
    Err       error
}
```

Sentinels:

```go
var (
    ErrNotStruct, ErrNilPointer, ErrMissingRequired,
    ErrFileNotSupported, ErrMaxDepthExceeded,
    ErrBodyTooLarge, ErrFileTooLarge
)
```

Patterns:

```go
// Match a class of error
if errors.Is(err, goform.ErrMissingRequired) {
    // respond 400 with "token is required"
}

// Pull out structured context
var de *goform.DecodingError
if errors.As(err, &de) {
    log.Printf("failed to decode key %q: %v", de.Key, de.Err)
}
```

Every failure from `Unmarshal` (and from the `Decoder` methods) is a
`*DecodingError` — including plain scalar parse failures like an int overflow or
a bad bool — so the `errors.As` branch above is exhaustive, not best-effort.

> `DecodingError`/`EncodingError` implement both `Error()` and `Unwrap()`, so
> `fmt.Errorf("...: %w", err)` and `errors.Is/As` both work through wrapping.

---

## 12. Concurrency

- The unified `Marshal`/`Unmarshal` create no shared state per call — safe from
  any goroutine.
- A single `Encoder`/`Decoder` is safe for concurrent use after construction.

---

## 13. Typical server flow (putting it together)

```go
func handleCreate(w http.ResponseWriter, r *http.Request) {
    body, _ := io.ReadAll(r.Body)
    var in CreateInput
    if err := goform.Unmarshal(body, r.Header.Get("Content-Type"), &in,
        goform.WithStrictUnmarshal(true), // reject unknown keys
        goform.WithMaxBodySize(1<<20),      // cap the body
        goform.WithMaxFileSize(5<<20),      // cap each file part
    ); err != nil {
        switch {
        case errors.Is(err, goform.ErrMissingRequired):
            http.Error(w, "missing required field", http.StatusUnprocessableEntity)
        case errors.Is(err, goform.ErrBodyTooLarge), errors.Is(err, goform.ErrFileTooLarge):
            http.Error(w, "payload too large", http.StatusRequestEntityTooLarge)
        default:
            http.Error(w, err.Error(), http.StatusBadRequest)
        }
        return
    }
    // ... persist in ...
}
```

---

## 14. Where to go next

- Run the examples: `go run ./_examples/basic`, `./_examples/nested`,
  `./_examples/multipart`, `./_examples/custom-types`,
  `./_examples/validation`.
- Read the full API reference via `go doc github.com/elsharaky/goform`.
- See the full behavioral spec in `doc.go`.
