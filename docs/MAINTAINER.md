# anyform — Maintainer Tutorial

A deep dive into how `anyform` is designed and built: the architecture, the
core algorithms, the public contract, and how to extend and test it.

If you're a user, see [DEVELOPER.md](DEVELOPER.md) instead. For a visual
overview, see [MINDMAP.md](MINDMAP.md).

---

## 1. Goals & design principles

`anyform` is a zero-dependency Go library for struct ↔ form-data conversion.

Design principles:

1. **HTTP-agnostic core.** No `net/http` import anywhere in `encoder.go`,
   `decoder.go`, `tag.go`, or `types.go`. HTTP concerns are the caller's job
   (content-type header), or delegated to small helpers in `file.go`.
2. **Two public layers, one engine.**
   - The **unified API** (`Marshal`/`Unmarshal`) does format auto-detection.
   - The **Encoder/Decoder** API is the same engine, pre-configured and
     reusable.
3. **Reflection-driven, configurable.** All behavior is driven by struct tags
   through a configurable priority system.
4. **Safe by default.** `Marshal`/`Unmarshal` allocate no shared mutable state;
   `Encoder`/`Decoder` are read-only after construction, hence thread-safe.
5. **Fail with context.** All errors carry a field path / key so callers can
   react to the exact field that failed.

---

## 2. Repository layout

```
anyform.go            Unified Marshal/Unmarshal + format detection + scanForFiles
encoder.go            Encoder, encodeStruct/Field/Slice/Map + multipart encode
decoder.go            Decoder, key-path tokenizer, unmarshal, defaults/required
tag.go                Tag priority resolver, tag parsing, unmarshal index
types.go              Built-in converters (time, duration, ip, url), scalar parse
options.go            config struct + functional options (With*)
errors.go             EncodingError, DecodingError, sentinel errors
file.go               File type + FileFromHeader / FilesFromRequest
doc.go                Package documentation (godesk reference)
_examples/            Runnable CLI examples (basic, nested, multipart, custom-types)
*_test.go             Unit, example, benchmark, and robustness tests
.github/              CI + release + code scanning workflows
```

---

## 3. The core types

### `config` (options.go)

```go
type config struct {
    tagPriority []string
    maxDepth    int
    maxBodySize int64
    maxFileSize int64
    zeroEmpty   bool
    timeLayout  string
    converters  map[reflect.Type]Converter
    textAware   bool
    strict      bool
}
```

Functional options mutate this struct. `defaultConfig()` seeds the built-in
converters (`time.Duration`, `net.IP`, `url.URL`) and default priority.
`maxBodySize` / `maxFileSize` are `0` by default (unlimited); `> 0` enables a
limit, `<= 0` disables it.

### `Encoder` / `Decoder`

```go
type Encoder struct {
    cfg      *config
    resolver *tagResolver
}
type Decoder struct {
    cfg      *config
    resolver *tagResolver
}
```

Both are built the same way: `newConfig(opts...)` then
`newTagResolver(cfg.tagPriority...)`. They hold no write state, so they are
safe for concurrent use after construction.

### `tagResolver` (tag.go, unexported)

Resolves field names from tags. Key methods:

- `marshalFieldName(sf) (name string, skip bool)` — the primary key.
- `marshalFieldOptions(sf) tagOptions` — omitempty/required/default flags.
- `buildUnmarshalIndex(t) map[string]reflect.StructField` — maps every tag name
  to its field, **flattening anonymous embedded structs** into the parent scope.
- `unmarshalFieldName(t, key) (field, ok)` — reverse lookup.

`tagOptions`:

```go
type tagOptions struct {
    Name, Default            string
    Skip, OmitEmpty, Required, HasDefault bool
}
```

---

## 4. The unified API and format detection (`anyform.go`)

```go
func Marshal(v any, opts ...Option) (body []byte, contentType string, err error) {
    cfg := newConfig(opts...)
    enc := &Encoder{cfg: cfg, resolver: newTagResolver(cfg.tagPriority...)}
    rv, err := addressableValue(v)
    ...
    if scanForFiles(rv, make(map[reflect.Type]bool), 0) { // has File fields?
        return enc.MarshalMultipart(v)
    }
    vals, _ := enc.Marshal(v)
    return []byte(vals.Encode()), "application/x-www-form-urlencoded", nil
}
```

### `scanForFiles`

Walks the struct recursively looking for `File` / `[]File` fields. It:

- Dereferences pointers and interfaces.
- Iterates slice/map/array elements.
- Guards against **self-referential types** with a `visited map[reflect.Type]bool`
  (a `*Node` pointing back to `Node` would otherwise recurse forever).
- Respects `defaultMaxDepth` as a hard stop.

If any `File` is found, `Marshal` routes to multipart; otherwise url-encoded.

```go
func Unmarshal(body []byte, contentType string, v any, opts ...Option) error {
    cfg := newConfig(opts...)
    dec := &Decoder{cfg: cfg, resolver: newTagResolver(cfg.tagPriority...)}
    if isMultipartContentType(contentType) {
        return dec.unmarshalMultipartBody(body, contentType, v)
    }
    values, err := url.ParseQuery(string(body))
    return dec.Unmarshal(values, v)
}
```

`unmarshalMultipartBody` parses the boundary from the Content-Type, uses
`multipart.NewReader` + `ReadForm(32<<20)`, then delegates to
`Decoder.UnmarshalMultipartForm`.

---

## 5. The encoder (`encoder.go`)

### url.Values path

```
Marshal(v) -> addressableValue(v) -> encodeStruct(rv, "", vals, 0)
```

- `encodeStruct` iterates struct fields:
  - Anonymous embedded structs → **flatten** by recursing into the child.
  - Unexported → skip.
  - `marshalFieldName` → skip if flagged.
  - `omitempty` / `zeroEmpty` → skip empty via `isEmpty`.
  - Otherwise `encodeField`.
- `encodeField` dereferences pointers, then:
  1. `File` fields → `ErrFileNotSupported` (url.Values can't hold files).
  2. Custom converter, then `time.Time` (honors layout), then `TextMarshaler`.
  3. Switch on kind: struct / slice / map / scalar / interface.
- `encodeSlice` and `encodeMap` build `key[i]` / `key[k]` paths.

### The depth bug this library avoided

Originally, nested structs reset the depth counter to `1` on every `encodeField`
-> `encodeStruct` hop, so the `WithMaxDepth` guard never fired for *named*
nesting and cyclic graphs could recurse unboundedly. **Depth is now threaded
through every encode function** (`encodeField(..., depth)`) so the guard applies
to structs, slices, maps, and containers alike. A self-referential struct now
returns `ErrMaxDepthExceeded` instead of overflowing the stack.

### multipart path

```
MarshalMultipart(v) -> encodeStructMultipart(rv, "", mw, 0)
```

- Uses `multipart.NewWriter` → `bytes.Buffer`.
- `File` / `[]File` fields are written as file parts via `writeFilePart`.
- Everything else via `writeStringPart` (scalars, converters, time).
- `mw.Close()` writes the terminating boundary; returns
  `mw.FormDataContentType()` (includes boundary).
- `MarshalMultipart` produces **valid multipart even with no File fields** —
  value fields simply become regular parts.

---

## 6. The decoder (`decoder.go`)

### Key-path tokenizer (`parseKeyPath`)

Form keys are parsed into tokens:

```go
type keyToken struct {
    kind string // "field" | "index" | "mapkey"
    name string
}

"name"          -> [{field name}]
"address.city"  -> [{field address} {field city}]
"items[0].name" -> [{field items} {index 0} {field name}]
"attr[key]"     -> [{field attr} {mapkey key}]
"matrix[0][1]"  -> [{field matrix} {index 0} {index 1}]
```

The tokenizer reads characters: `.` flushes the current name; `[`...`]` parses
inner text as an integer index or a map key.

### Unmarshal path

```
Unmarshal(values, v) -> unmarshalValues(values, elem, 0) -> applyDefaultsAndRequired
```

- `unmarshalValues` builds the unmarshal index for the struct level, iterates
  submitted keys, tokenizes each, and calls `decodePath`.
- `decodePath` walks the tokens, allocating pointers, descending into structs,
  setting slice indexes, and reading map keys. Leaf assignment goes through
  `assignLeaf` → `assignScalarTo` (converters, `TextUnmarshaler`, `parseScalar`).
- `assignLeaf` handles structs (time.Time via converter), slices (append /
  positional), maps (must use bracket notation), and scalars.

### multipart path

```
UnmarshalMultipartForm(mf, v)
  -> unmarshalValues(url.Values(mf.Value), elem, 0)  // scalar/value fields
  -> unmarshalFiles(mf, elem)                        // File fields
  -> applyDefaultsAndRequired
```

`unmarshalFiles` walks the struct, matches file part names, and populates
`File` / `[]File` fields from `readFile`, a thin wrapper around
`FileFromHeader` that enforces `config.maxFileSize`:
if `len(f.Content) > maxFileSize`, it returns `DecodingError{ErrFileTooLarge}`
and the whole input is rejected (there is no partial-file behavior).

Size limits are **post-parse**: files are read fully first, then size-checked.
This removes the unbounded-RAM problem but doesn't stream. A future
streaming path could reject oversized parts before buffering.

### defaults & required (`applyDefaultsAndRequired`)

Runs *after* a successful decode:

- Builds the set of submitted base keys (`valuesKeys` / `multipartKeys`).
- Walks the struct, recursing into nested and anonymous structs.
- A field is "provided" if a key equals its name or prefixes it with `.` / `[`.
- If **not** provided:
  - `required` → `ErrMissingRequired` (wrapped in `DecodingError{Key: name}`).
  - `default:v` → sets the value via `assignScalarTo`, but **only for scalar
    kinds** (`isDefaultable` excludes pointers, slices, maps, files).
- This makes `default`/`required` work for both url.Values and multipart, and
  for nested levels.

---

## 7. Type handling (`types.go`)

- **Built-in converters** register themselves in `defaultConfig`:
  - `durationConverter` — `time.Duration` ↔ `"1h30m"` via `ParseDuration`.
  - `ipConverter` — `net.IP` ↔ string.
  - `urlConverter` — `url.URL` ↔ string.
  - `timeConverter` — `time.Time`, but with configurable layout. `time.Time`
    is handled *before* the generic `TextMarshaler` branch so a custom
    `WithTimeLayout` is respected.
- **`parseScalar`** converts strings to all scalar kinds via `strconv`, with
  overflow checks and informative errors.
- **`assignScalarTo`** prefers `TextUnmarshaler`, then custom converter, then
  `parseScalar`.

---

## 8. The `File` type (`file.go`)

```go
type File struct {
    Content     []byte
    ContentType string
    Filename    string
}
```

- Decoupled from HTTP on purpose — usable in handlers, tests, gRPC, CLIs.
- `FileFromHeader(fh)` opens a multipart header, reads all bytes, and sniffs a
  Content-Type if the header lacks one.
- `FilesFromRequest(r, field)` pulls `[]File` for a named field.
- Historically there was a `type Files = []File` alias; it was **removed** for
  a cleaner API — users write `[]File` directly.

---

## 9. The public contract

The exported surface is intentionally minimal:

```
Marshal / Unmarshal                 top-level unified API
NewEncoder / Encoder.Marshal / .MarshalMultipart
NewDecoder / Decoder.Unmarshal / .UnmarshalMultipart / .UnmarshalMultipartForm
File, Converter, Option
With* options (9)
EncodingError, DecodingError, ErrNotStruct, ErrNilPointer,
ErrMissingRequired, ErrFileNotSupported, ErrMaxDepthExceeded,
ErrBodyTooLarge, ErrFileTooLarge
FileFromHeader, FilesFromRequest
```

Everything reflection-internal (the tag resolver) is **unexported** — users
never touch `reflect.StructField` plumbing.

### Stability commitments

- The unified API and Encoder/Decoder are the stable public surface.
- Tag semantics are documented in `doc.go` — treat them as a contract.
- Errors implement `Unwrap()` so `errors.Is/As` work through wrapping.

---

## 10. Testing strategy

- **Unit tests** (`encoder_decoder_test.go`, `tag_test.go`) — table-driven,
  cover tag priority, nested types, slices, maps, errors.
- **Feature-specific tests** (`defaults_test.go`, `multipart_gap_test.go`) —
  `default`/`required`, multipart-with-no-files, missing boundary.
- **Robustness tests** (`robustness_test.go`) — `WithZeroEmpty` semantics,
  circular-reference safety, and **concurrency** (many goroutines sharing one
  Encoder/Decoder, plus top-level concurrent calls).
- **Example tests** (`examples_test.go`) — runnable, godoc-verified `// Output`.
- **Benchmarks** (`bench_test.go`) — `BenchmarkMarshal_*` / `BenchmarkUnmarshal_*`.

### The safety toolbox

```bash
go test -race ./...
go vet ./...
golangci-lint run        # configured for golangci-lint v2
gosec ./...
govulncheck ./...
```

CI (`.github/workflows/ci.yml`) runs all of these on every PR. Security
scanning (CodeQL), release automation (`main.yml` running semantic-release,
which tags `vX.Y.Z` and publishes the GitHub Release from Conventional
Commits; no goreleaser since this is a pure library), and dependabot are
configured.

---

## 11. How to extend

- **New supported type** → usually handled automatically by reflection; or add
  a built-in converter in `types.go` and register it in `defaultConfig`.
- **New tag option** → add a field to `tagOptions`, parse it in
  `parseTagOptions`, and consume it in the encoder/decoder.
- **New option** → add a field to `config` and a `With*` func in `options.go`.
- **Format detection changes** → `scanForFiles` / `isMultipartContentType`.
- Always update `doc.go` (the contract), add tests, and run the toolbox.

---

## 12. Common pitfalls to remember

- Depth must be threaded through *every* recursion point, or cyclic structs
  break the stack and `WithMaxDepth` silently stops working.
- `time.Time` must be handled *before* the generic `TextMarshaler` branch or
  custom layouts are ignored.
- `default` uses `isDefaultable` — applying it to a pointer/slice/map returns
  an error; keep defaults to scalars.
- `scanForFiles` needs its `visited` map, or self-referential types hang.
- Unmarshal accepts any tag name; Marshal uses priority order. They are
  intentionally asymmetric — do not "fix" that to be symmetric.
