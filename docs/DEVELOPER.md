# anyform — Developer Tutorial

A practical, step-by-step guide to using `anyform` in your Go application. By
the end you'll be able to marshal structs into request bodies and unmarshal
request bodies back into structs, including file uploads, nested types, and
custom converters.

If you're implementing or maintaining `anyform` itself, see
[MAINTAINER.md](MAINTAINER.md).

---

## 1. What anyform does

`anyform` serializes Go structs to and from **HTML form data** in two flavors:

- **`application/x-www-form-urlencoded`** — flat `key=value` strings.
- **`multipart/form-data`** — used whenever the data contains file uploads.

The library is **zero-dependency** and **HTTP-agnostic** (it never imports
`net/http` in its core). You give it a `[]byte` body plus a `Content-Type`, and
it figures out the format for you — or you produce exactly that pair.

---

## 2. Installation

```bash
go get github.com/elsharaky/anyform
```

Requires **Go 1.27+** (`any` type, `reflect.StructField.IsExported`, etc.).

---

## 3. The quick start

The entire library surfaces as two functions:

```go
import "github.com/elsharaky/anyform"

type User struct {
    Name  string `form:"name"`
    Email string `form:"email"`
    Age   int    `form:"age"`
}

// Marshal: struct -> (body bytes, Content-Type)
body, ct, err := anyform.Marshal(User{Name: "Alice", Email: "alice@example.com", Age: 30})
// body == "age=30&email=alice%40example.com&name=Alice"
// ct   == "application/x-www-form-urlencoded"

// Unmarshal: (body bytes, Content-Type) -> struct
var user User
err := anyform.Unmarshal(body, ct, &user)
```

That's the whole mental model. `Marshal` returns the exact pair you'd put in a
request or response; `Unmarshal` consumes the exact pair you'd receive.

### Using it with net/http

```go
// Client side
body, ct, _ := anyform.Marshal(payload)
resp, _ := http.Post(url, ct, bytes.NewReader(body))

// Server side
readBody, _ := io.ReadAll(r.Body)
var payload Payload
if err := anyform.Unmarshal(readBody, r.Header.Get("Content-Type"), &payload); err != nil {
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
| `WithTimeLayout(layout)` | `time.RFC3339` | `time.Time` format string |
| `WithZeroEmpty(bool)` | `false` | Omit all zero-valued fields on marshal |
| `WithCustomConverter(type, conv)` | built-ins | Override a type's (un)marshal |
| `WithTextMarshalerSupport(bool)` | `true` | Auto `TextMarshaler`/`TextUnmarshaler` |
| `WithStrictUnmarshal(bool)` | `false` | Error on unknown submitted keys |
| `WithMaxBodySize(bytes)` | unlimited | Max body size → `ErrBodyTooLarge` |
| `WithMaxFileSize(bytes)` | unlimited | Max per-file size → `ErrFileTooLarge` |

Example:

```go
body, ct, _ := anyform.Marshal(
    event,
    anyform.WithTimeLayout("2006-01-02"),
    anyform.WithZeroEmpty(true),
)
```

### Reusable Encoder / Decoder

When you configure the same options everywhere (a typical server), build a
shared instance instead. They are safe for concurrent use.

```go
dec := anyform.NewDecoder(
    anyform.WithStrictUnmarshal(true),
    anyform.WithTimeLayout("2006-01-02"),
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
  unmarshal. Check with `errors.Is(err, anyform.ErrMissingRequired)`.
- `,default:v` — populate an absent **scalar** field with `v` on unmarshal.

---

## 7. Supported Go types

`anyform` handles essentially everything:

- **Scalars** — `string`, `bool`, all `int`/`uint`/`float`/`complex` widths.
- **Named types** — `type MyInt int`, `type Status string`.
- **Pointers** — `*T` (nil omitted on marshal, allocated on unmarshal).
- **Slices / arrays** — indexed keys.
- **Maps** — bracket-named keys.
- **Structs** — nested and embedded structs.
- **Time** — `time.Time`, `time.Duration`.
- **Networking** — `net.IP`, `url.URL`.
- **Text interfaces** — any `encoding.TextMarshaler`/`TextUnmarshaler`.
- **Files** — `File` and `[]File`.

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

body, ct, _ := anyform.Marshal(Order{
    ID:       42,
    ShipTo:   Address{City: "Lyon", ZIP: "69001"},
    Lines:    []string{"A", "B"},
    Discount: map[string]string{"code": "SAVE10"},
})
// body: discount%5Bcode%5D=SAVE10&id=42&line%5B0%5D=A&line%5B1%5D=B&ship_to.city=Lyon&ship_to.zip=69001

var got Order
anyform.Unmarshal(body, ct, &got) // reconstructs everything
```

> Repeating a scalar key (`k=a&k=b`) yields a `[]string` / slice on unmarshal.

---

## 9. File uploads

`anyform.File` decouples file handling from `net/http`:

```go
type File struct {
    Content     []byte // raw bytes
    ContentType string // MIME type
    Filename    string // original name
}
```

Define a field as `File` (single) or `[]File` (multiple). `Marshal` **auto-
detects** the `File` fields and switches to multipart automatically.

```go
type Upload struct {
    Title  string         `form:"title"`
    Avatar anyform.File   `form:"avatar"`
    Docs   []anyform.File `form:"documents"`
}

// Client
body, ct, err := anyform.Marshal(Upload{
    Title:  "Report",
    Avatar: anyform.File{Filename: "report.pdf", Content: []byte("%PDF"), ContentType: "application/pdf"},
    Docs:   []anyform.File{{Filename: "n.txt", Content: []byte("n"), ContentType: "text/plain"}},
})

// Server (HTTP-agnostic decode)
var up Upload
err := anyform.Unmarshal(body, ct, &up)
fmt.Println(up.Avatar.Filename, string(up.Avatar.Content))
```

> **Note:** file parts should carry a `Filename` for reliable multipart
> detection. A part with an empty filename may be classified as a value field.

> **Security note:** the library reads each file fully into memory and, by
> default, imposes **no size limit**. For untrusted uploads, cap both the
> request (`http.MaxBytesReader`) and per-file size
> (`WithMaxFileSize` → `ErrFileTooLarge`):

```go
var up Upload
err := anyform.Unmarshal(body, ct, &up,
    anyform.WithMaxBodySize(1<<20*10),  // whole body ≤ 10 MiB
    anyform.WithMaxFileSize(1<<20*5),   // each file ≤ 5 MiB
)
if errors.Is(err, anyform.ErrFileTooLarge) {
    // respond 413 Payload Too Large
}
```

### Working from an `http.Request` directly

Two helpers read files out without a full decode:

```go
files, err := anyform.FilesFromRequest(req, "documents") // []File
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

enc := anyform.NewEncoder(anyform.WithCustomConverter(reflect.TypeOf(Status(0)), statusConverter{}))
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
if errors.Is(err, anyform.ErrMissingRequired) {
    // respond 400 with "token is required"
}

// Pull out structured context
var de *anyform.DecodingError
if errors.As(err, &de) {
    log.Printf("failed to decode key %q: %v", de.Key, de.Err)
}
```

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
    dec := anyform.NewDecoder(anyform.WithStrictUnmarshal(true))

    body, _ := io.ReadAll(r.Body)
    var in CreateInput
    if err := anyform.Unmarshal(body, r.Header.Get("Content-Type"), &in); err != nil {
        if errors.Is(err, anyform.ErrMissingRequired) {
            http.Error(w, "missing required field", http.StatusUnprocessableEntity)
            return
        }
        http.Error(w, err.Error(), http.StatusBadRequest)
        return
    }
    // ... persist in ...
    _ = dec
}
```

---

## 14. Where to go next

- Run the examples: `go run ./_examples/basic`, `./_examples/multipart`, etc.
- Read the full API reference via `go doc github.com/elsharaky/anyform`.
- See the full behavioral spec in `doc.go`.
