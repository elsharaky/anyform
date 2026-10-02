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
doc.go                Package documentation (godoc reference)
README.md             Project overview, feature matrix, quick start
CONTRIBUTING.md       Contribution + release-marker conventions
docs/                 DEVELOPER.md (user guide), MAINTAINER.md (this), MINDMAP.md
_examples/            Runnable CLI examples (basic, nested, multipart,
                      custom-types, validation)
*_test.go             Unit, example, benchmark, and robustness tests
.github/              CI + release + code scanning workflows
```

---

## 3. The core types

### `config` (options.go)

```go
type config struct {
    tagPriority   []string
    maxDepth      int
    maxBodySize   int64
    maxFileSize   int64
    maxSliceIndex int
    zeroEmpty     bool
    timeLayout    string
    converters    map[reflect.Type]Converter
    textAware     bool
    strict        bool
}
```

Functional options mutate this struct. `defaultConfig()` seeds the built-in
converters (`time.Duration`, `net.IP`, `url.URL`), the default tag priority, and
the limits: `maxDepth` 32, `maxSliceIndex` 100000.
`maxBodySize` / `maxFileSize` are `0` by default (unlimited); `> 0` enables a
limit, `<= 0` disables it. `maxSliceIndex` behaves the same way, with `0`
meaning unlimited.

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

- `marshalFieldName(sf) (name string, skip bool)` — the primary key (highest
  priority tag present; skip when that tag is `-`).
- `marshalFieldOptions(sf) tagOptions` — omitempty/required/default flags.
- `isSkipped(sf) bool` — the skip decision on its own.
- `buildUnmarshalIndex(t) unmarshalIndex` — **cached** per type; maps every tag
  name (plus the Go field name) to its field, flattening anonymous embedded
  structs into the parent scope. See `unmarshalIndex` below.

```go
type unmarshalIndex struct {
    fields    map[string]reflect.StructField // every tag alias -> field
    ambiguous map[string]bool                 // key claimed by >1 field
}
```

Ambiguity is recorded at build time so both the value decoder and the file
decoder reject a colliding key instead of letting the last writer win. There is
no `unmarshalFieldName(t, key)` helper: lookups always go through the per-type
index, which is what makes flattening, aliases, and ambiguity detection work
the same at every nesting level.

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
    if scanForFiles(rv, make(map[reflect.Type]bool), 0, cfg.maxDepth, enc.resolver) {
        return enc.MarshalMultipart(v)
    }
    vals, _ := enc.Marshal(v)
    return []byte(vals.Encode()), urlEncodedContentType, nil
}
```

### `scanForFiles`

Walks the value recursively looking for `File` / `[]File` fields. It:

- Dereferences pointers and interfaces.
- Iterates slice/array elements and map values.
- Skips fields the encoder would skip — unexported, `form:"-"`, explicit omit —
  so the format decision tracks what multipart would actually emit.
- Guards against **self-referential types** with a `visited map[reflect.Type]bool`
  (a `*Node` pointing back to `Node` would otherwise recurse forever).
- Respects the configured `maxDepth` as a hard stop.
- Uses a **call-stack** visited set, deleting the type on the way out: the same
  struct type appearing twice in sibling branches is scanned both times (the
  dynamic content can differ), while a true cycle is still cut.

If any `File` is found, `Marshal` routes to multipart; otherwise url-encoded.

```go
func Unmarshal(body []byte, contentType string, v any, opts ...Option) error {
    cfg := newConfig(opts...)
    if cfg.maxBodySize > 0 && int64(len(body)) > cfg.maxBodySize {
        return &DecodingError{Err: ErrBodyTooLarge}
    }
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
Unmarshal(values, v) -> unmarshalValues(values, elem, 0)
                  -> applyDefaultsAndRequired(elem, providedFields(elem, formKeys(values), 0))
```

- `unmarshalValues` builds the unmarshal index for the struct level, iterates
  submitted keys, tokenizes each, and calls `decodePath`.
- `decodePath` walks the tokens, allocating pointers, descending into structs,
  setting slice indexes, and reading map keys. Leaf assignment goes through
  `assignLeaf` → `assignScalarTo` (converters, `TextUnmarshaler`, `parseScalar`).
- `assignLeaf` handles structs (`time.Time` via converter, a scalar aimed at a
  struct is an error), slices (`[]byte` single value, append / positional), maps
  (must use bracket notation), and scalars.
- A client `[i]` key grows a slice only up to `cfg.maxSliceIndex`; beyond it the
  key fails with a `DecodingError`.

### multipart path

```
UnmarshalMultipartForm(mf, v)
  -> unmarshalValues(url.Values(mf.Value), elem, 0)  // scalar/value fields
  -> unmarshalFiles(mf, elem)                        // File fields
  -> applyDefaultsAndRequired(elem, providedFields(elem, valueKeys + filePartNames, 0))
```

`unmarshalFiles` routes each part name through the **same key path** the value
decoder uses, via `consumeFilePart` → `consumeFilePartTokens`. A part is
tokenized with `parseKeyPath` and walked token by token, so:

- `meta.avatar` descends into a nested struct, mirroring what the encoder
  emitted.
- `docs[0].bin` grows/addresses a slice element.
- `m[k]` / `m[k].bin` creates map entries.
- Embedded promoted fields and **every tag alias** (`form`, `json`, `xml`,
  `protobuf`, plus the Go field name) resolve identically to value keys.

The final token must land on a `File`, `*File`, or `[]File` leaf. Anything else
leaves the part unconsumed — dropped by default, rejected under
`WithStrictUnmarshal`. An ambiguous base (two sibling `File` fields sharing a
tag, or an embedded promoted `File` colliding with an outer one) is an error in
**both** modes: a colliding part would otherwise be consumed by every matching
field.

Parts are visited in sorted order so that when two different part names resolve
to the same field, the winner is deterministic across runs.

Fields are populated from `readFile`, a thin wrapper around `FileFromHeader`
that enforces `config.maxFileSize`: if `fh.Size > maxFileSize`, it returns
`DecodingError{ErrFileTooLarge}` **before** the content is read into memory, and
the whole input is rejected (there is no partial-file behavior). `FileFromHeader`
reads the content with `io.ReadAll`, and the check against `len(f.Content)`
serves as a safety net for hand-built `FileHeader`s whose `Size` field is zero.

A **value** part addressed at a `File` field is ignored rather than fatal: that
is the signature of an untouched browser file input, whose part carries an empty
filename and lands in the value store. Failing the decode there would break an
ordinary form with an optional file box left empty.

Size limits are checked **pre-read**: an oversized part is rejected on its
declared size before any buffering. This removes the unbounded-RAM problem
without adding a streaming read path. The unified `Unmarshal` additionally
checks `WithMaxBodySize` against `len(body)` up front.

### defaults & required (`applyDefaultsAndRequired`)

Runs *after* a successful decode, and takes a **provided set** rather than
re-deriving "was this submitted?" from the key strings:

- `providedFields(dst, keys, depth)` parses each submitted key and routes it
  through `markProvidedPath`, which mirrors `decodePath`'s routing and records
  the canonical `StructField.Index` chain (`joinIndex`) of every field the key
  resolves to. For multipart, the key list is the value keys **plus** the file
  part names, so a field satisfied only by a file part counts as provided.
- Because routing is shared, a nested key (`ship_to.city`), an indexed key, a
  promoted embedded field, or an alternate tag name all mark the destination
  field provided. A key the decoder rejects (ambiguous, unknown, out of range,
  too deep) marks nothing — matching the fact that it decodes to nothing.
- Walks the struct, recursing into nested, anonymous, and pointer-embedded
  structs.
- For a field that is **not** provided:
  - `required` → `ErrMissingRequired` (wrapped in `DecodingError{Key: name}`).
  - `default:v` → sets the value via `assignScalarTo`, but **only for scalar
    kinds** (`isDefaultable` excludes pointers, slices, maps, and `File`).

This is what makes `default`/`required` work for both `url.Values` and
multipart, at every nesting level, without a default ever overwriting a value
the client actually sent.

---

## 7. Type handling (`types.go`)

- **Built-in converters** register themselves in `defaultConfig`:
  - `durationConverter` — `time.Duration` ↔ `"1h30m"` via `ParseDuration`.
  - `ipConverter` — `net.IP` ↔ string.
  - `urlConverter` — `url.URL` ↔ string.
  - `timeConverter` — `time.Time`, but with configurable layout. `time.Time`
    is handled *before* the generic `TextMarshaler` branch so a custom
    `WithTimeLayout` is respected.
- **`parseScalar`** converts strings to all scalar kinds via `strconv`, at the
  destination's own bit size, with overflow checks and informative errors. A
  `float32` field receiving `1e40` errors instead of becoming `+Inf`.
- **`assignScalarTo`** prefers a registered converter, then `TextUnmarshaler`,
  then `parseScalar`.
- **`[]byte` is one scalar**, in both directions: the encoder emits its raw
  text (`data=hello`) instead of expanding it into per-byte keys, and
  `assignLeaf` fills it from a single submitted value. Explicit `[i]` keys still
  address individual elements.

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
- `File`, `*File`, and `[]File` are all valid field types and round-trip at any
  nesting.
- `FileFromHeader(fh)` opens a multipart header, reads all bytes, and sniffs a
  Content-Type if the header lacks one.
- `FilesFromRequest(r, field)` pulls `[]File` for a named field, returning
  `nil, nil` (not an error) when the field has no files.
- Encoder/decoder symmetry at the zero boundary: a `File` with an empty
  filename (e.g. the zero value) is **skipped** on emit, a part with an empty
  filename is a *value* part to the parser, and a value part addressed at a
  `File` field is ignored. A part that *is* present with zero bytes still
  binds, keeping its filename — so a body goform produces always round-trips.
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
With* options (10): WithTagPriority, WithMaxDepth, WithMaxSliceIndex,
                    WithTimeLayout, WithZeroEmpty, WithCustomConverter,
                    WithTextMarshalerSupport, WithStrictUnmarshal,
                    WithMaxBodySize, WithMaxFileSize
EncodingError, DecodingError, ErrNotStruct, ErrNilPointer,
ErrMissingRequired, ErrFileNotSupported, ErrMaxDepthExceeded,
ErrBodyTooLarge, ErrFileTooLarge
FileFromHeader, FilesFromRequest
```

`With*` accepts an explicit `Option` for both the top-level functions and the
`New*` constructors, and a nil option is skipped.

Everything reflection-internal (the tag resolver) is **unexported** — users
never touch `reflect.StructField` plumbing.

### Stability commitments

- The unified API and Encoder/Decoder are the stable public surface.
- Tag semantics are documented in `doc.go` — treat them as a contract.
- Errors implement `Unwrap()` so `errors.Is/As` work through wrapping.

---

## 10. Testing strategy

- **`marshal_test.go` / `decoder_test.go`** — the bulk of the suite
  (~66 tests): tag priority, nesting, slices, arrays, maps, pointers, embedded
  and pointer-embedded structs, ambiguity, strict mode, scalar parsing, and
  `WithMaxSliceIndex` (cap, custom cap, disabled). `decoder_test.go` also
  carries the `,default` / `,required` matrix (nested, aliased, and promoted
  variants).
- **`tag_test.go`** — resolver behavior: priority order, field names, tag
  option parsing, key-path tokenizing, and the per-type index cache.
- **`files_test.go`** — `File` behavior: multipart round-trips at every nesting
  (nested, slice, map, embedded, pointer), file/[]File/*File tag aliases,
  ambiguous parts, strict-mode parts, zero-byte bodies, and the
  empty-filename/value-part parity rules.
- **`limits_test.go`** — `WithMaxBodySize`, `WithMaxFileSize` (including
  reject-before-read and whole-input rejection), the global `WithZeroEmpty`, and
  the multipart edge cases (no file fields, boundary in the Content-Type, missing
  boundary).
- **`converter_test.go`** — built-in converters plus `WithCustomConverter`
  precedence at every container depth.
- **`roundtrip_test.go`** — marshal → unmarshal symmetry: multipart, `[]byte`
  single-value, numeric-key maps, and `TextMarshaler` in string and struct kinds.
- **`goform_test.go`** — the top-level API and multipart auto-detection.
- **`robustness_test.go`** — circular-reference safety (depth error, and
  `scanForFiles` neither hanging nor wrongly caching a type across sibling
  interface fields) and **concurrency** (many goroutines sharing one
  Encoder/Decoder, plus top-level concurrent calls).
- **Example tests** (`examples_test.go`) — 11 runnable, output-verified
  `Example*` functions; these are the godoc examples, so their `// Output`
  blocks are part of the test suite.
- **Benchmarks** (`bench_test.go`) — 6 `BenchmarkMarshal_*` / `BenchmarkUnmarshal_*`.
- **Runnable programs** (`_examples/`) — five `main` packages users can `go run`;
  they are not covered by `go test ./...` (underscore dirs are ignored by the
  go tool), so run them by hand when touching the public API.

### The safety toolbox

```bash
go test -race ./...
go vet ./...
gofmt -l .            # must print nothing
golangci-lint run     # configured for golangci-lint v2
gosec ./...
govulncheck ./...

# _examples/ is excluded from ./... by the leading underscore — run it directly.
for d in _examples/*/; do go run "./$d"; done
```

CI (`.github/workflows/ci.yml`) runs gofmt, build, vet, race tests,
golangci-lint, govulncheck, and gosec on every PR and push to `main`; gosec
reports are uploaded to the Security tab via SARIF. CodeQL and dependabot are
configured. Release automation lives in `.github/workflows/main.yml` — see
[Release process](#11-release-process) below; no goreleaser since this is a pure
library.

---

## 11. Release process

This project uses [semantic-release](https://semantic-release.gitbook.io) with
an **explicit-release model**: ordinary work merges never release by themselves;
releases are triggered only by a dedicated release-marker commit. This section
explains the workflow in `.github/workflows/main.yml`, the rules in
`.releaserc`, and the exact semantics of each.

### 11.1 The trigger

The `Version` workflow fires on every `push` to `main` — **not** on
`pull_request: closed`. This is a hard constraint of semantic-release, not a
style choice: env-ci reports `isPr=true` for *any* `pull_request` event (even
`closed`), and semantic-release's core then logs "This run was triggered by a
pull request and therefore a new version won't be published." and exits
without tagging. Only a `push` event (`isPr=false`, branch `main`) can
release — which the merge of a PR into `main` always is. Tag pushes
(`refs/tags/*`) don't match `branches: [main]`, so semantic-release pushing
the next tag never re-triggers the workflow.

A `release` concurrency group serializes runs so two close pushes can't race
on tag creation. The job checks out the merged state of `main`
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
- **Release body:** `release-notes-generator` lists every commit in the window,
  grouped by type (`feat` → Features, `fix` → Bug Fixes, `perf` → Performance
  Improvements); `chore`/`ci`/`docs`/`refactor` are hidden by the
  `conventionalcommats` preset, so housekeeping stays out of the notes. This
  output is the *generated* body only — the project changelog is
  [`CHANGELOG.md`](../CHANGELOG.md), written by hand.

Consequences:

- A `feat:`/`fix:` merged before a marker is automatically swept into that
  release's notes — you never hand-pick what ships.
- A `feat:`/`fix:` merged **after** a release tag belongs to the *next* window
  and waits for the next marker.
- Marker commits themselves produce no changelog line; they are the version
  decision, not content.

The generated body is **not the record** — [`CHANGELOG.md`](../CHANGELOG.md) is.
The generator has two hard limits:

- **Hidden types never appear.** The `conventionalcommits` preset marks
  `docs`/`style`/`chore`/`refactor`/`test`/`build`/`ci` as `hidden: true`, so if
  a marker's window contains only those, the body is generated empty. That
  happened on `v0.1.0` (18 commit lines, over half CI plumbing) and again on
  `v0.1.1`.
- **Commit subjects are all it has.** It never opens the diff, so it cannot say
  what a change *means* for a reader — the thing Keep a Changelog asks for.

Hence the split: entries are written by hand in the PR that makes the change,
under `## [Unreleased]`, where the context is still fresh. See
[CONTRIBUTING.md](../CONTRIBUTING.md#changelog). Two conventions:

1. Label real user-facing work `feat:`/`fix:`/`perf:`, never bury it under
   `chore:` — this is what the generated body reflects.
2. After a marker merges, open the new release and compare its body against the
   matching `CHANGELOG.md` section; paste the section in if the generated body
   is thinner.

### 11.4 The first release (semantic-release quirk)

`semantic-release` hardcodes the **very first** release to `1.0.0`. To honor
SemVer and start this project at `0.1.0`, the workflow seeds an annotated
`v0.0.0` tag on the **root commit** exactly when no `v[0-9]*` tags exist yet
(no-op on later runs). The first `release(minor)` marker then bumps `0.0.0 →
0.1.0`. The seed push is why `RELEASE_TOKEN` (not the default `GITHUB_TOKEN`)
is required — see [11.1](#111-the-trigger).

**Historical artifact:** the root commit still carries an old tag-push
`Release` workflow (`.github/workflows/release.yml`, `on: push: tags: ['v*']`).
Because GitHub resolves workflow files at the commit a pushed tag *points to*,
the first seed push of `v0.0.0` (at the root commit) triggered that stale
workflow once, creating a GitHub *Release* named `v0.0.0` with auto-generated
"Full Changelog" notes. That release is cosmetic noise — the real versioning
signal is the **tag** `v0.0.0`, and semantic-release derives the next version
from tags, not releases. It can be deleted from the Releases page without
affecting future releases; later tags point at newer commits whose trees no
longer contain that workflow, so it will never run again.

### 11.5 Making a release, step by step

0. **Prerequisite (first release only):** ensure the `RELEASE_TOKEN` secret
   exists (Settings → Secrets and variables → Actions). It is a fine-grained
   PAT with Contents: read/write + Workflows: read/write on this repo. The
   workflow's `git push` runs as this token; the default `GITHUB_TOKEN`
   cannot push refs that touch `.github/workflows/` (see [11.1](#111-the-trigger)).
1. Ensure the work to ship is merged into `main` (`feat:`/`fix:`/`perf:`
   commits — they require no marker to land), and that each shipped change has
   an entry under `## [Unreleased]` in `CHANGELOG.md`. The generated body is
   *not* the record, so an unentered change ships with no changelog line at all.
2. **Move `[Unreleased]` under a new version heading** — `## [0.2.0] - YYYY-MM-DD`,
   newest first — and add the compare link at the foot of the file. This is a
   docs-only commit; it needs no marker.
3. Create a branch off `main`, add a **message-only** empty commit
   (e.g. `git commit --allow-empty -m "release(minor): ship ..."`), open a PR,
   merge it.
4. The workflow tags `main` `v<computed>` and publishes a GitHub Release whose
   notes include all accumulated user-facing work.
5. Verify the tag and release on GitHub, then **reconcile the release body**
   with the version section you just wrote: paste it in if the generated body
   is thinner (see [11.3](#113-the-commit-window)).
6. Patch-level follow-ups repeat the process with a `release(patch)` marker.

### 11.6 Anti-patterns

- **Naming work as markers.** Labeling a real change `release(...)` pollutes
  the commit-window logic; markers must be message-only.
- **Multiple markers in one window.** Two markers of different scopes ship as
  the higher scope; use one marker per release window.
- **Bumping versions by hand.** Versioning is owned by the pipeline; a manual
  version bump or tag will collide with `semantic-release`.
- **Treating the generated release body as the changelog.** It is derived from
  commit labels, so it is empty for all-hidden windows and it can only repeat
  commit subjects. `CHANGELOG.md` is the record; the body is a pointer to it.
- **Trying to automate this away.** `presetConfig.types` can un-hide `docs`, and
  `@semantic-release/changelog` can write the file — but both inherit the same
  commit-subject ceiling, and the first would make every `docs:` commit trigger
  a release, breaking the marker model. See [11.3](#113-the-commit-window).

---

## 12. How to extend

- **New supported type** → usually handled automatically by reflection; or add
  a built-in converter in `types.go` and register it in `defaultConfig`.
- **New tag option** → add a field to `tagOptions`, parse it in
  `parseTagOptions`, and consume it in the encoder/decoder.
- **New option** → add a field to `config` and a `With*` func in `options.go`.
- **Format detection changes** → `scanForFiles` / `isMultipartContentType`.
- **New File nesting** → file routing goes through the same key-path tokens as
  values, so extending it is usually a change in `consumeFilePartTokens`, not a
  new naming convention.
- Always update `doc.go` (the contract), the README, `docs/`, and
  `docs/MINDMAP.md`, add tests, and run the toolbox.

---

## 13. Common pitfalls to remember

- Depth must be threaded through *every* recursion point, or cyclic structs
  break the stack and `WithMaxDepth` silently stops working.
- `time.Time` must be handled *before* the generic `TextMarshaler` branch or
  custom layouts are ignored.
- `default` uses `isDefaultable` — keep defaults to scalars; pointers, slices,
  maps, and `File` are excluded.
- `scanForFiles` needs its `visited` map, or self-referential types hang.
- File-size limits must be checked on the **declared** `fh.Size` *before*
  reading, or an oversized part is buffered first.
- A client-supplied `[i]` key is a body-size-independent allocation vector:
  `WithMaxSliceIndex` is the only bound on it.
- Marshal and Unmarshal share one index per type. When you add a lookup path,
  route it through the cached index or ambiguity detection and tag aliases stop
  applying at that level.
- Unmarshal accepts any tag name; Marshal uses priority order. They are
  intentionally asymmetric — do not "fix" that to be symmetric.
- Anything in `_examples/` is invisible to `go build ./...` and `go test ./...`.
  A broken example will not fail CI.
