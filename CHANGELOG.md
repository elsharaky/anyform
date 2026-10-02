# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

While `goform` is pre-1.0, a minor bump may contain breaking changes.

## [Unreleased]

### Added

- `CHANGELOG.md`, following [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
  Entries are written by hand in the PR that makes the change, because the
  generated release notes are derived from commit labels and cannot describe
  what a change means for a reader.
- `CONTRIBUTING.md`: a `Changelog` section stating the convention, and a
  changelog checkbox in the PR template.

## [0.1.1] - 2026-09-30

Documentation-only. No library code changed: every exported symbol, option, and
error behaves exactly as in [0.1.0].

### Fixed

- `doc.go`: the file-upload sample used `goform.Files`, a type that does not
  exist, so the documented example did not compile. It now shows
  `[]goform.File`, and states that `*File` is accepted alongside `File` and
  `[]File`.
- `doc.go`: `[]byte` decodes as a single opaque value (`data=hello` fills the
  whole slice) rather than one key per byte. Now documented, including that
  explicit `[i]` keys still address individual elements.
- `doc.go`: `WithMaxSliceIndex` shipped undocumented. Its default (100000), the
  reason it exists (a client-supplied `[i]` key forces an allocation from a
  body that can be a few bytes long), and the resulting `*DecodingError` are
  now stated.
- `doc.go`: the guarantee that every decode failure is a `*DecodingError` was
  too narrow. It holds for plain scalar parse failures too, not only nested
  container lookups.
- `README.md`: `,default:` was described as resolved only against a field's
  primary `form` name. A field counts as provided when *any* of its tag names
  resolves to it, so a default is never written over a value the client sent —
  including via a nested key or a tag alias.
- `README.md`: the supported-types table omitted `[]byte` and `*File`.
- `docs/DEVELOPER.md`: claimed Go 1.27 as the minimum; `go.mod` declares
  `go 1.22`. CI builds and tests on Go 1.27.
- `docs/DEVELOPER.md`: `FileFromHeader` and `WithMaxSliceIndex` were missing
  from the API inventory.
- `docs/MAINTAINER.md`: rewritten. The `config` struct was missing
  `maxSliceIndex` (9 options listed, 10 exist), and the descriptions of tag
  resolution, file routing, and default/required handling predated the current
  decoder.
- `docs/MINDMAP.md`: fixed a misspelled identifier (`marshaledFieldName` →
  `marshalFieldName`), removed `unmarshalFieldName`, which does not exist, and
  added the `isSkipped` and `firstExistingTag` helpers that are actually
  called. The `Unmarshal` signature omitted its target argument, and a stale
  `[NEW]` marker on `ErrBodyTooLarge` was cleared.
- `_examples/custom-types`: its header claimed custom tag-priority behaviour
  the example never exercised.

### Added

- `_examples/validation`: a runnable example for `,required`, `,default`,
  `,omitempty`, `WithStrictUnmarshal`, and `WithMaxSliceIndex` — all previously
  documented without a working example.
- `CONTRIBUTING.md`: documents that release bodies are generated from commit
  labels, that hidden types can leave a body empty, and that a thin body should
  be edited by hand after a marker merges.

## [0.1.0] - 2026-09-29

First release.

### Added

- `goform.Marshal` and `goform.Unmarshal` — the unified entry points. `Marshal`
  takes a struct and returns a body plus its `Content-Type`; `Unmarshal` takes a
  body and a `Content-Type` and fills a struct. Both detect url-encoded versus
  multipart on their own.
- `Encoder` and `Decoder` — configured instances, for when you want to set
  options once and reuse them. `Encoder.Marshal` / `MarshalMultipart` return
  `url.Values` or a multipart body; `Decoder.Unmarshal` /
  `UnmarshalMultipart` / `UnmarshalMultipartForm` accept url-encoded values, an
  `*http.Request`, or a `*multipart.Form`.
- Tag-driven field mapping with a configurable priority. `form`, `json`, `xml`,
  and `protobuf` tags are all read: the first one present wins when
  marshalling, and a key matching *any* of them is accepted when unmarshalling.
  The Go field name is the final fallback. Reorder with `WithTagPriority`.
- `File` — a file upload carrying `Content`, `ContentType`, and `Filename`,
  decoupled from `net/http`. Usable as `File`, `*File`, or `[]File`, at any
  nesting, and routed by the same key paths as ordinary values. `FileFromHeader`
  and `FilesFromRequest` bridge from `net/http` when you want that coupling.
- Support for essentially every Go type as a field: primitives, named types,
  pointers, slices, arrays, maps, nested structs, embedded structs (value and
  pointer), `time.Time`, `time.Duration`, `net.IP`, `url.URL`, `[]byte` as a
  single opaque value, and anything implementing
  `encoding.TextMarshaler` / `TextUnmarshaler`.
- `Converter` and `WithCustomConverter` — register your own marshal/unmarshal
  pair per type. A registered converter always beats the text interfaces, at
  every container depth, on both encode and decode.
- Tag options `,omitempty`, `,required`, and `,default:value`. `required` and
  `default` apply on unmarshalling only, and `default` never overwrites a value
  the client actually sent.
- Hardening options: `WithMaxDepth` (default 32), `WithMaxSliceIndex`
  (default 100000), `WithMaxBodySize`, and `WithMaxFileSize`. An over-limit file
  part is rejected from its declared size before its content is read into
  memory. `WithStrictUnmarshal` additionally rejects unknown keys instead of
  ignoring them.
- Seven sentinel errors — `ErrMissingRequired`, `ErrNilPointer`,
  `ErrNotStruct`, `ErrMaxDepthExceeded`, `ErrBodyTooLarge`, `ErrFileTooLarge`,
  `ErrFileNotSupported` — usable with `errors.Is`, plus `*EncodingError` and
  `*DecodingError`, which carry the offending `FieldPath` and `Key` and unwrap
  to the cause.
- Ambiguous-key rejection: when two different fields resolve to the same
  submitted key, decoding fails rather than picking one. This holds without
  strict mode.
- Four runnable examples under `_examples/`: `basic`, `nested`, `multipart`, and
  `custom-types`.
- Requires Go 1.22. No external dependencies. Safe for concurrent use.

[unreleased]: https://github.com/elsharaky/goform/compare/v0.1.1...HEAD
[0.1.1]: https://github.com/elsharaky/goform/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/elsharaky/goform/compare/v0.0.0...v0.1.0