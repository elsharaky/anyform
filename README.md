# anyform

Seamless, tag-driven marshalling and unmarshalling of Go structs to and from
form data, URL-encoded values, and multipart form data — with file uploads,
a configurable tag-priority system, and comprehensive type support.

`anyform` is a drop-in replacement for form libraries like
[`gorilla/schema`](https://github.com/gorilla/schema) and
[`go-playground/form`](https://github.com/go-playground/form), filling the gaps
those libraries leave behind.

[![Go Reference](https://pkg.go.dev/badge/github.com/elsharaky/anyform.svg)](https://pkg.go.dev/github.com/elsharaky/anyform)
[![Go Report Card](https://goreportcard.com/badge/github.com/elsharaky/anyform)](https://goreportcard.com/report/github.com/elsharaky/anyform)
[![CI](https://github.com/elsharaky/anyform/actions/workflows/ci.yml/badge.svg)](https://github.com/elsharaky/anyform/actions)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

## Features

- **One-line, unified API** — `anyform.Marshal` / `anyform.Unmarshal` handle
  url-encoded and multipart bodies automatically (body + Content-Type in,
  struct out), no format plumbing required.
- **Seamless marshal + unmarshal** — structs to `url.Values` / multipart bodies
  and back, or pre-configured `Encoder`/`Decoder` instances for fine control.
- **Tag priority system** — use `form`, `json`, `xml`, and `protobuf` tags
  together; the first tag found wins, with the Go field name as the final
  fallback. Fully configurable.
- **Native file support** — the [`File`](#the-file-type) type carries content,
  content type, and filename, decoupled from `net/http`.
- **Every Go type as a field** — primitives, pointers, slices, arrays, maps,
  nested structs, embedded structs, `time.Time`, `time.Duration`, custom types,
  and anything implementing `encoding.TextMarshaler`/`TextUnmarshaler` (the
  root value must be a struct — see *Root value* below).
- **Zero external dependencies.**
- **Thread-safe** — both the unified functions and `Encoder`/`Decoder`.

## Why anyform?

Competing libraries are missing key functionality:

| Feature | gorilla/schema | go-playground/form | **anyform** |
|---|---|---|---|
| Marshal + Unmarshal | ✅ | ✅ | ✅ |
| Tag priority system | ❌ | ❌ | **✅** |
| Native file type | ❌ | ❌ | **✅** |
| Map support | ❌ | ✅ | **✅** |
| `omitempty` | ❌ | ✅ | **✅** |
| `required` | ✅ | ❌ | **✅** |
| `default` values | ✅ | ❌ | **✅** |
| `TextMarshaler` support | ✅ | ❌ | **✅** |
| Multipart native | ❌ | ❌ | **✅** |
| Zero dependencies | ✅ | ✅ | **✅** |

## Installation

```bash
go get github.com/elsharaky/anyform
```

## Documentation

- **[Developer tutorial](docs/DEVELOPER.md)** — step-by-step guide to using the
  library in your application.
- **[Maintainer tutorial](docs/MAINTAINER.md)** — architecture, internals, and
  how to extend the package.
- **[Mind map](docs/MINDMAP.md)** — visual overview of the whole package.
- `go doc github.com/elsharaky/anyform` — full API reference.

## Quick Start

### The unified API (recommended)

`Marshal` and `Unmarshal` do all the work behind the scenes. `Marshal` returns
a body plus its `Content-Type`, **auto-detecting** the format: multipart when
the struct contains `File` fields, url-encoded otherwise. `Unmarshal` decodes a
body back into a struct, detecting the format from `Content-Type`.

```go
import "github.com/elsharaky/anyform"

type User struct {
    Name  string `form:"name"`
    Email string `form:"email"`
    Age   int    `form:"age"`
}

// Marshal: struct -> []byte body + Content-Type
body, ct, err := anyform.Marshal(User{Name: "Alice", Email: "alice@example.com", Age: 30})
//   no File fields -> ct == "application/x-www-form-urlencoded"

// Unmarshal: []byte body + Content-Type -> struct
var user User
err := anyform.Unmarshal(body, ct, &user)
```

Both functions accept functional options for a single call, and are safe to
call from any goroutine (no shared state).

### Encoder / Decoder

For reusable, pre-configured instances, use the `Encoder` / `Decoder` API:

```go
enc := anyform.NewEncoder(anyform.WithTimeLayout(time.RFC3339), anyform.WithStrictUnmarshal(true))
vals, err := enc.Marshal(user)             // -> url.Values
body, ct, err := enc.MarshalMultipart(v)   // -> multipart (explicit)

dec := anyform.NewDecoder()
err := dec.Unmarshal(vals, &v)                        // from url.Values
err := dec.UnmarshalMultipart(req, &v)                // from *http.Request
err := dec.UnmarshalMultipartForm(form, &v)           // from *multipart.Form
```

`Encoder` and `Decoder` are safe for concurrent use after construction.

## Tag Priority

Fields may carry multiple tags. During **marshalling**, the first tag in the
priority order that exists on the field is used:

```go
type Product struct {
    ID   int    `json:"product_id"`                    // -> product_id (json)
    Name string `form:"product_name" json:"product_name"` // -> product_name (form wins)
    Slug string `xml:"slug" json:"slug"`               // -> slug (json beats xml)
    Note string                                        // -> Note (Go field name)
}
```

**Default priority:** `form > json > xml > protobuf`

Override it per-encoder:

```go
enc := anyform.NewEncoder(anyform.WithTagPriority("json", "form", "protobuf"))
```

During **unmarshalling**, whichever key the client sends is matched against
all tag names, so any supported tag name works as the submitted key.

## The `File` type

`anyform.File` carries everything about an uploaded file:

```go
type File struct {
    Content     []byte // raw file content
    ContentType string // MIME type
    Filename    string // original name
}
```

```go
type Upload struct {
    Title  string         `form:"title"`
    Avatar anyform.File   `form:"avatar"`   // single file
    Docs   []anyform.File `form:"documents"` // multiple files
}

// Marshal: anyform detects the File fields and produces multipart/form-data
body, contentType, err := anyform.Marshal(upload)

// Unmarshal: anyform detects multipart from the Content-Type and populates
// File/[]File fields — no net/http dependency, no manual ParseMultipartForm.
err := anyform.Unmarshal(body, contentType, &upload)
```

`File` is decoupled from `net/http`, so it works in handlers, tests, gRPC,
CLIs, and more. Use `FilesFromRequest` to pull raw files from an
`http.Request` without a decoder. For reliable multipart detection, file parts
should carry a filename.

A file part that **is** present but carries **zero bytes** is still bound: the
resulting `File` has an empty `Content` but preserves its `Filename`. That is
intentional — an empty upload is treated as a provided (empty) file, not as an
absent one. A part with an *empty* filename, by contrast, is classified by the
multipart parser as a value field (see the [hardening](#server-hardening)
notes).

## Supported types

> **Root value:** the value passed to `Marshal` / `Unmarshal` (and to
> `Encoder`/`Decoder`) must be a **struct** (or a pointer to a struct) — the
> same contract `encoding/json` has for the destination, but `anyform` keeps it
> for **both** directions. Slices, arrays, maps, and primitives are supported
> only as *field values*, because every form key names a field: a bare root
> slice would have no namespace to attach its keys to.

| Category | Types | Notes |
|----------|-------|-------|
| Primitives | `string`, `bool`, `int*`, `uint*`, `float*`, `complex*` | parsed via `strconv` |
| Named types | `type MyInt int`, `type Status string` | via reflection |
| Pointers | `*T` | nil → omitted; empty → nil |
| Slices/Arrays | `[]T`, `[N]T` | indexes: `field[0]` |
| Maps | `map[K]V` | keys: `field[key]` |
| Nested structs | dot notation | e.g. `address.city` |
| Embedded structs | flattened | promoted fields |
| `time.Time` | RFC3339 default | configurable layout |
| `time.Duration` | `"1h30m"` | |
| `net.IP`, `url.URL` | string form | |
| `TextMarshaler` / `TextUnmarshaler` | automatic | |
| `File` / `[]File` | multipart | |
| Custom types | `WithCustomConverter` | |
| `any` / `interface{}` | encode only | see note below |

> **`any` fields:** encoding a field typed as `any`/`interface{}` (holding a
> concrete value, e.g. a string or number) works and serializes the concrete
> value. **Decoding is not supported**: there is no way to know which concrete
> type to parse into, so an incoming value targeting an `any` field fails with
> `unsupported field kind interface`.

## Marshaller / unmarshaller behavior

### Key format

The form keys produced and accepted follow this convention:

| Example key | Meaning |
|-------------|---------|
| `name` | field name |
| `address.city` | nested struct field |
| `items[0]` | slice/array index |
| `items[0].name` | index, then a field |
| `attr[key]` | map key |
| `matrix[0][1]` | nested indexes |

Repeating a scalar key (`k=a&k=b`) yields a `[]string` / slice on unmarshal.

### Nested structs, slices, and maps, end to end

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
anyform.Unmarshal(body, ct, &got) // reconstructs all nested fields
```

### Marshaller semantics

- A field is **skipped** when the first tag in priority order is `-`
  (`json:"-"` with an earlier `form` tag still participates via `form`).
- `,omitempty` (and the global `WithZeroEmpty`) omit empty values on marshal.
- Zero values are emitted by default (`age=0`, `name=`) unless omitted.

### Unmarshaller semantics

- `,required` returns `ErrMissingRequired` when the field is absent.
- `,default:v` populates an absent scalar field with `v`.
- `WithStrictUnmarshal(true)` reports unknown keys as errors — including
  unknown multipart file parts, not just value keys.
- Any tag name in the priority list is accepted as the submitted key, for
  value fields **and** `File`/`[]File` fields.
- Nil pointers are allocated; absent fields keep their zero value.

### Server hardening

`anyform` reads bodies and file content into memory, and by default imposes no
size limits. For untrusted uploads the strongest protection stops oversized
requests **before** any parsing — wrap the request body in
[`http.MaxBytesReader`](https://pkg.go.dev/net/http#MaxBytesReader), then use
`WithMaxBodySize` / `WithMaxFileSize` as a second line of defense:

```go
// 10 MiB cap on the whole request, enforced while reading.
r.Body = http.MaxBytesReader(w, r.Body, 10<<20)
form, err := r.MultipartForm()
if err != nil {
    // e.g. 413 Payload Too Large
}
var up Upload
err = anyform.NewDecoder(
    anyform.WithMaxBodySize(10<<20),
    anyform.WithMaxFileSize(5<<20), // each file ≤ 5 MiB
).UnmarshalMultipartForm(form, &up)
if errors.Is(err, anyform.ErrFileTooLarge) {
    // 413 Payload Too Large
}
```

Because file parts are checked against their declared size **before** their
content is read into memory, an oversized file is rejected without first
buffering it.

A second, body-size-independent vector is a client-supplied slice index:
a tiny key like `items[1000000]=x` would otherwise grow a `[]string` to a
million elements. `WithMaxSliceIndex` bounds this per-field (default 100000);
indices at or above the bound fail with a `DecodingError`.

## Configuration

| Option | Description |
|--------|-------------|
| `WithTagPriority(...tags)` | Custom tag order (default `form > json > xml > protobuf`) |
| `WithMaxDepth(n)` | Max nested struct depth (default 32) |
| `WithMaxSliceIndex(n)` | Max index a slice may grow to from a `[i]` key (default 100000, 0 = unlimited) |
| `WithTimeLayout(layout)` | Layout for `time.Time` (default `RFC3339`) |
| `WithZeroEmpty(bool)` | Omit zero-valued fields during marshal, like a global `omitempty` (default false) |
| `WithCustomConverter(type, conv)` | Register a custom `Converter` |
| `WithTextMarshalerSupport(bool)` | Enable/disable `TextMarshaler` (default true) |
| `WithStrictUnmarshal(bool)` | Error on unknown keys (default false) |
| `WithMaxBodySize(bytes)` | Max body size in `Unmarshal` → `ErrBodyTooLarge` (0 = unlimited) |
| `WithMaxFileSize(bytes)` | Max per-file size in multipart decode → `ErrFileTooLarge` (0 = unlimited). Rejected using the part's declared size **before** the content is read into memory |

### Custom converters

```go
type Status int
const (Pending Status = iota; Active; Blocked)

type statusConverter struct{}
func (statusConverter) Marshal(v reflect.Value) (string, error) { /* ... */ }
func (statusConverter) Unmarshal(s string, f reflect.Value) error { /* ... */ }

enc := anyform.NewEncoder(anyform.WithCustomConverter(reflect.TypeOf(Status(0)), statusConverter{}))
```

## Tag options

Tags support `,omitempty`, `,required`, and `,default:value`:

```go
type Config struct {
    Nickname string `form:"nick,omitempty"`
    Token    string `form:"token,required"`            // errors with ErrMissingRequired if absent
    Region   string `form:"region,default:us-east"`    // applied when the field is absent
}
```

`omitempty` (and the global `WithZeroEmpty`) control marshalling output.
`required` and `default` are enforced during unmarshalling: a required field
that is missing produces `ErrMissingRequired`, and an absent field with a
default is populated before the key is returned. Both are resolved against the
field's primary `form` name.

## Examples

Standalone runnable examples live in [`_examples/`](_examples):

```bash
go run ./_examples/basic
go run ./_examples/multipart
go run ./_examples/nested
go run ./_examples/custom-types
```

## Benchmarks

```bash
go test -bench=. -run=^$ .
```

## Development

```bash
go test -race ./...
go vet ./...
golangci-lint run
gosec ./...
govulncheck ./...
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for contribution guidelines and the
[CI pipeline](.github/workflows/ci.yml) for the security, linting, and test
checks that run on every pull request.

## Security

To report a security vulnerability, please read [SECURITY.md](SECURITY.md).

## License

[MIT](LICENSE)
