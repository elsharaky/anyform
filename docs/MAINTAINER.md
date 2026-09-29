# goform — Maintainer Tutorial

A deep dive into how `goform` is designed and built: the architecture, the
core algorithms, the public contract, and how to extend and test it.

If you're a user, see [DEVELOPER.md](DEVELOPER.md) instead. For a visual
overview, see [MINDMAP.md](MINDMAP.md).

---

## 1. Goals & design principles

`goform` is a zero-dependency Go library for struct ↔ form-data conversion.

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
goform.go            Unified Marshal/Unmarshal + format detection + scanForFiles
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

## 4. The unified API and format detection (`goform.go`)

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

`unmarshalFiles` walks the struct and matches file part names using
`unmarshalTagNames` — the *same* alias logic value fields use (`form`, `json`,
`xml`, `protobuf`, plus the Go field name). A file part named by any of a
field's tags is accepted. Under `WithStrictUnmarshal`, file parts that match
no `File` field are rejected (previously they were silently dropped — a bug
fixed alongside the value/file alias parity issue). Fields are populated from
`readFile`, a thin wrapper around `FileFromHeader` that enforces
`config.maxFileSize`:
if `fh.Size > maxFileSize`, it returns `DecodingError{ErrFileTooLarge}`
**before** the content is read into memory, and the whole input is rejected
(there is no partial-file behavior). `FileFromHeader` reads the content with
`io.ReadAll`, and the check against `len(f.Content)` serves as a safety net for
hand-built `FileHeader`s whose `Size` field is zero.

Size limits are checked **pre-read**: an oversized part is rejected on its
declared size before any buffering. This removes the unbounded-RAM problem
without adding a streaming read path. The unified `Unmarshal` additionally
checks `WithMaxBodySize` against `len(body)` up front.

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
scanning (CodeQL) and dependabot are configured. Release automation lives in
`.github/workflows/main.yml` — see [Release process](#11-release-process) below;
no goreleaser since this is a pure library.

---

## 11. Release process

This project uses [semantic-release](https://semantic-release.gitbook.io) with
an **explicit-release model**: ordinary work merges never release by themselves;
releases are triggered only by a dedicated release-marker commit. This section
explains the workflow in `.github/workflows/main.yml`, the rules in
`.releaserc`, and the exact semantics of each.

### 11.1 The trigger

The `Version` workflow fires on `pull_request` closed + **merged** into `main`
(no release on bare pushes), with a `release` concurrency group so two close
merges can't race on tag creation. It checks out the merged state of `main`
(`fetch-depth: 0`) and runs `go test -race ./...` as a sanity re-run; the real
quality gate is the required `ci` check on the PR itself (vet, lint, gosec,
govulncheck, race tests) enforced by branch protection.

**Secret requirement:** the job checks out with `secrets.RELEASE_TOKEN`, a
fine-grained PAT (Contents: read/write, Workflows: read/write, scoped to this
repo), and passes it to semantic-release. This is mandatory, not optional:
GitHub hard-blocks the default `GITHUB_TOKEN` — a GitHub App credential with no
`workflows` scope — from pushing any git ref whose range touches
`.github/workflows/`. Our seed tag points at the root commit (which *creates*
the workflow files), so a `GITHUB_TOKEN` push is rejected server-side no matter
what `permissions: contents: write` says. The PAT must exist before the first
release; without it the seed step fails and no release happens.

### 11.2 The rules (`.releaserc`)

`releaseRules` are evaluated **in order, first match wins**, per commit:

1. `release(patch)` / `release(minor)` / `release(major)` — the **markers**;
   they are the only commits that can produce a release. Their scope picks the
   bump.
2. `breaking: true → release: false`, `fix/feat/perf/revert → release: false` —
   ordinary work is pinned to "no release" so it only accumulates.

Since semantic-release analyzes **all** commits since the last release tag, the
highest-scoped marker in the window decides the bump (e.g. a `release(patch)`
window containing a `release(major)` produces a major). The marker is the "go"
signal, never the only content of a release.

### 11.3 The commit window

`semantic-release` does not look at the merged PR alone. It collects **every
commit reachable from `main` since the last non-prerelease release tag** (the
seeded baseline for the first release, see [11.4](#114-the-first-release-semantic-release-quirk)) and then:

- **Version:** `commit-analyzer` maps each commit through `releaseRules` and
  takes the highest bump found (the marker scope).
- **Changelog:** `release-notes-generator` lists every commit in the window,
  grouped by type (`feat` → Features, `fix` → Bug Fixes, `perf` → Performance
  Improvements); `chore`/`ci`/`docs`/`refactor` are hidden by the
  `conventionalcommits` preset, so housekeeping stays out of the notes.

Consequences:

- A `feat:`/`fix:` merged before a marker is automatically swept into that
  release's notes — you never hand-pick what ships.
- A `feat:`/`fix:` merged **after** a release tag belongs to the *next* window
  and waits for the next marker.
- Marker commits themselves produce no changelog line; they are the version
  decision, not content.

### 11.4 The first release (semantic-release quirk)

`semantic-release` hardcodes the **very first** release to `1.0.0`. To honor
SemVer and start this project at `0.1.0`, the workflow seeds an annotated
`v0.0.0` tag on the **root commit** exactly when no `v[0-9]*` tags exist yet
(no-op on later runs). The first `release(minor)` marker then bumps `0.0.0 →
0.1.0`. The seed push is why `RELEASE_TOKEN` (not the default `GITHUB_TOKEN`)
is required — see [11.1](#111-the-trigger).

### 11.5 Making a release, step by step

0. **Prerequisite (first release only):** ensure the `RELEASE_TOKEN` secret
   exists (Settings → Secrets and variables → Actions). It is a fine-grained
   PAT with Contents: read/write + Workflows: read/write on this repo. The
   workflow's `git push` runs as this token; the default `GITHUB_TOKEN`
   cannot push refs that touch `.github/workflows/` (see [11.1](#111-the-trigger)).
1. Ensure the work to ship is merged into `main` (`feat:`/`fix:`/`perf:`
   commits — they require no marker to land).
2. Create a branch off `main`, add a **message-only** empty commit
   (e.g. `git commit --allow-empty -m "release(minor): ship ..."`), open a PR,
   merge it.
3. The workflow tags `main` `v<computed>` and publishes a GitHub Release whose
   notes include all accumulated user-facing work.
4. Verify the tag and release on GitHub; patch-level follow-ups repeat the
   process with a `release(patch)` marker.

### 11.6 Anti-patterns

- **Naming work as markers.** Labeling a real change `release(...)` pollutes
  the commit-window logic; markers must be message-only.
- **Multiple markers in one window.** Two markers of different scopes ship as
  the higher scope; use one marker per release window.
- **Bumping versions by hand.** Versioning is owned by the pipeline; a manual
  version bump or tag will collide with `semantic-release`.

---

## 12. How to extend

- **New supported type** → usually handled automatically by reflection; or add
  a built-in converter in `types.go` and register it in `defaultConfig`.
- **New tag option** → add a field to `tagOptions`, parse it in
  `parseTagOptions`, and consume it in the encoder/decoder.
- **New option** → add a field to `config` and a `With*` func in `options.go`.
- **Format detection changes** → `scanForFiles` / `isMultipartContentType`.
- Always update `doc.go` (the contract), add tests, and run the toolbox.

---

## 13. Common pitfalls to remember

- Depth must be threaded through *every* recursion point, or cyclic structs
  break the stack and `WithMaxDepth` silently stops working.
- `time.Time` must be handled *before* the generic `TextMarshaler` branch or
  custom layouts are ignored.
- `default` uses `isDefaultable` — applying it to a pointer/slice/map returns
  an error; keep defaults to scalars.
- `scanForFiles` needs its `visited` map, or self-referential types hang.
- Unmarshal accepts any tag name; Marshal uses priority order. They are
  intentionally asymmetric — do not "fix" that to be symmetric.
