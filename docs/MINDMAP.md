# goform — Package Mind Map

A visual, text-based map of the package: what it is, its public API, internal
architecture, and how the pieces hang together.

```
┌─────────────────────────────────────────────────────────────────────────┐
│                            goform (package)                            │
│      Zero-dependency Go struct <-> form-data (url-encoded + multipart)  │
└─────────────────────────────────────────────────────────────────────────┘

  LEGEND
  ──────
  [FUNC]   exported function / method
  [TYPE]   exported type
  <OPT>    exported option
  [i]      internal / unexported
  ✔        user-facing API
  •        internal detail
```

## Public surface (what users touch)

```
Marshal / Unmarshal              ✔  unified, format auto-detection
NewEncoder() -> Encoder          ✔  reusable config
   .Marshal(v) -> url.Values
   .MarshalMultipart(v) -> body + ct
NewDecoder() -> Decoder          ✔
   .Unmarshal(url.Values, &v)
   .UnmarshalMultipart(req, &v)
   .UnmarshalMultipartForm(mf, &v)
File{Content, ContentType, Filename}  ✔  the file type
File / *File / []File fields            ✔  at any nesting
Converter interface                  ✔  custom (un)marshal
FileFromHeader, FilesFromRequest     ✔  file helpers (net/http aware)
EncodingError / DecodingError        ✔  contextual errors
Err* sentinels                       ✔  errors.Is targets

<OPT> options (functional):
  WithTagPriority  WithMaxDepth  WithMaxSliceIndex  WithTimeLayout
  WithZeroEmpty    WithCustomConverter
  WithTextMarshalerSupport  WithStrictUnmarshal
  WithMaxBodySize  WithMaxFileSize
```

## Unified API (goform.go)

```
Marshal(v, opts...)
 ├─ newConfig(opts...)
 ├─ addressableValue(v)             • deref pointers, error if not struct
 ├─ scanForFiles(rv, visited, 0,
 │              cfg.maxDepth, r)    • recursive File scan, call-stack cycle guard
 │                                  • skips unexported / form:"-" fields
 ├─ has File -> MarshalMultipart (multipart/form-data + boundary)
 └─ no File  -> Encoder.Marshal -> []byte(vals.Encode()) + urlencoded
Unmarshal(body, ct, v, opts...)
 ├─ maxBodySize? body too big -> ErrBodyTooLarge
 ├─ isMultipartContentType(ct)?
 │    └─ yes -> unmarshalMultipartBody: parse boundary -> multipart.Reader
 │               -> ReadForm(32<<20) -> UnmarshalMultipartForm
 └─ no  -> url.ParseQuery -> Decoder.Unmarshal
```

## Encoder (encoder.go)

```
Encoder.Marshal(v) -> vals                          (url.Values)
Encoder.MarshalMultipart(v) -> []byte, ct           (multipart.Writer)

encodeStruct(rv, prefix, vals, depth)
 ├─ anonymous embedded struct -> flatten (recurse)
 ├─ unexported -> skip
 ├─ marshalFieldName -> skip?
 ├─ omitempty / WithZeroEmpty -> isEmpty?
 └─ encodeField(rv, key, vals, depth)   • depth threaded everywhere
      ├─ File / []File -> ErrFileNotSupported (url.Values path)
      ├─ custom converter
      ├─ time.Time (layout)   BEFORE TextMarshaler
      ├─ []byte -> one scalar blob, not per-byte keys
      ├─ TextMarshaler (if enabled)
      ├─ struct -> encodeStruct(depth+1)
      ├─ slice/array -> encodeSlice  (key[i])
      ├─ map        -> encodeMap     (key[k])
      └─ scalar     -> formatScalar (vals.Add)
  (multipart twin: encodeStructMultipart / encodeFieldMultipart
   writeFilePart for File, writeStringPart for the rest;
    a File with an empty filename is skipped -> bodies round-trip)

Depth guard at every entry -> ErrMaxDepthExceeded (cycle-safe)
```

## Decoder (decoder.go)

```
Decoder.Unmarshal(vals, &v)
Decoder.UnmarshalMultipart(req, &v)   -> UnmarshalMultipartForm
Decoder.UnmarshalMultipartForm(mf, &v)

parseKeyPath(key) -> []keyToken{kind: field|index|mapkey}

unmarshalValues(vals, elem, depth)
 └─ buildUnmarshalIndex(type) -> unmarshalIndex{fields, ambiguous}
          • cached per type; flattens anon structs; every tag alias
          • ambiguous[key] -> DecodingError (both modes, no strict needed)
     └─ for each submitted key: decodePath(tokens)

decodePath(field, rest[], values, depth)
 ├─ deref pointers (allocate if nil)
 ├─ leaf -> assignLeaf
 │         ├─ struct   -> time.Time via converter
 │         │                value part at a File field -> ignored
 │         │                scalar at a struct field  -> DecodingError
 │         ├─ slice     -> []byte single value / append / positional
 │         ├─ map       -> requires bracket notation
 │         └─ scalar   -> assignScalarTo
 │                     ├─ custom converter
 │                     ├─ TextUnmarshaler (if enabled)
 │                     └─ parseScalar (strconv, own bit size)
 ├─ field  -> descend into nested struct
 ├─ index  -> slice/array element (bounded by maxSliceIndex)
 └─ mapkey -> map entry

unmarshalFiles(mf, elem)   • populate File / *File / []File from parts
 ├─ parts visited in sorted order -> deterministic routing
 ├─ consumeFilePart -> consumeFilePartTokens
 │     • SAME key paths as values: meta.avatar, docs[0].bin, m[k].bin
 │     • tag aliases + promoted embedded fields resolve identically
 │     • ambiguous base -> DecodingError (both modes)
 │     • final token must be a File leaf, else unconsumed
 ├─ strict -> unconsumed part = DecodingError
 └─ readFile(fh)   • maxFileSize checked on fh.Size BEFORE read
     over-limit -> DecodingError{ErrFileTooLarge} (whole input rejected)
     (len(f.Content) check kept as fallback for hand-built FileHeaders)

providedFields(elem, keys, depth)   • after decode
 └─ markProvidedPath: mirrors decodePath routing, records canonical
     field index paths (joinIndex); multipart adds file part names

applyDefaultsAndRequired(elem, provided, indexPath, depth)   • after decode
 ├─ required & missing      -> ErrMissingRequired
 └─ default:v & missing     -> assignScalarTo (scalars only, isDefaultable)
```

## Tag system (tag.go) [i]

```
tagResolver (unexported)
 ├─ priority []string        default: form > json > xml > protobuf
 ├─ marshalFieldName(sf)     (name, skip)
 ├─ marshalFieldOptions(sf)  tagOptions
 ├─ isSkipped(sf)            skip decision on its own
 ├─ buildUnmarshalIndex(t)   cached unmarshalIndex (see above)
 └─ firstExistingTag(sf)     first tag in priority order

parseTagOptions("name,omitempty,required,default:v")
 ├─ protobuf "wire,num,name=xxx" special-case
 ├─ "-" -> Skip
 └─ omitempty / required / default:v

Skip semantics: skip iff FIRST existing tag in priority is "-".
Marshal uses first existing tag; Unmarshal accepts ANY tag name.
```

## Types & scalar parsing (types.go)

```
parseScalar(s, field)   • strconv for bool/int*/uint*/float*/complex*
                          at the field's own bit size, overflow-checked
assignScalarTo          • converter -> TextUnmarshaler -> parseScalar
built-in converters     • registered in defaultConfig:
     durationConverter  time.Duration  (ParseDuration)
     ipConverter        net.IP
     urlConverter       url.URL
     timeConverter      time.Time      (WithTimeLayout, before TextMarshaler)
```

## Configuration (options.go)

```
config{ tagPriority, maxDepth(32), maxBodySize(0=∞), maxFileSize(0=∞),
        maxSliceIndex(100000), zeroEmpty, timeLayout(RFC3339),
        converters, textAware(true), strict }
newConfig(opts...) -> defaultConfig() + apply options
```

## Errors (errors.go)

```
EncodingError{FieldPath, Err}    Error() + Unwrap()
DecodingError{FieldPath, Key, Err}  Error() + Unwrap()
ErrNotStruct  ErrNilPointer  ErrMissingRequired
ErrFileNotSupported  ErrMaxDepthExceeded
ErrBodyTooLarge  ErrFileTooLarge
```

## Files (file.go)

```
File{Content []byte, ContentType, Filename}
  usable as File / *File / []File, at any nesting
FileFromHeader(*multipart.FileHeader) -> File
FilesFromRequest(*http.Request, field) -> []File   (nil, nil when absent)
(HTTP-aware ONLY here; core is HTTP-agnostic)
```

## Package docs & examples

```
doc.go        • package contract (types, key format, semantics)
_examples/    • basic | nested | multipart | custom-types | validation
              • excluded from ./... by the leading underscore - run directly
examples_test.go • 11 godoc-verified Example* outputs
CHANGELOG.md  • hand-maintained changelog (Keep a Changelog); the release
                body generated by semantic-release is NOT this file
docs/         • DEVELOPER.md (user guide)  MAINTAINER.md (architecture)
```

## Data-flow summary (one glance)

```
         struct ──Marshal──▶ (body []byte, Content-Type)
              ▲                     │
              │                     ▼
         struct ◀──Unmarshal── (body []byte, Content-Type)
          (File detection picks multipart vs urlencoded automatically)
```

## Areas to keep an eye on (maintainer)

```
 • Depth threading across ALL recursion points (cycle safety)
 • time.Time handled before TextMarshaler (layout respect)
 • default: only for scalars (isDefaultable)
 • scanForFiles visited-map cycle guard (call-stack, not global)
 • File limits checked on fh.Size BEFORE reading content
 • WithMaxSliceIndex: "[i]" keys are a body-size-independent vector
 • Every lookup path goes through the cached unmarshalIndex
 • Marshal priority vs Unmarshal any-tag asymmetry (intentional)
 • Errors always wrap with Unwrap() for errors.Is/As
```
