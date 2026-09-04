# anyform — Package Mind Map

A visual, text-based map of the package: what it is, its public API, internal
architecture, and how the pieces hang together.

```
┌─────────────────────────────────────────────────────────────────────────┐
│                            anyform (package)                            │
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
[]File                               ✔  multi-file (replaces old Files alias)
Converter interface                  ✔  custom (un)marshal
FileFromHeader, FilesFromRequest     ✔  file helpers (net/http aware)
EncodingError / DecodingError        ✔  contextual errors
Err* sentinels                       ✔  errors.Is targets

<OPT> options (functional):
  WithTagPriority  WithMaxDepth  WithTimeLayout
  WithZeroEmpty    WithCustomConverter
  WithTextMarshalerSupport  WithStrictUnmarshal
  WithMaxBodySize  WithMaxFileSize
```

## Unified API (anyform.go)

```
Marshal(v, opts...)
 ├─ newConfig(opts...)
 ├─ addressableValue(v)          • deref pointers, error if not struct
 ├─ scanForFiles(rv, visited, 0) • recursive File/[]File scan, cycle-guarded
 │    ├─ has File -> MarshalMultipart (multipart/form-data + boundary)
 │    └─ no File  -> Encoder.Marshal -> []byte(vals.Encode()) + urlencoded
Unmarshal(body, ct, opts...)
 ├─ maxBodySize? body too big -> ErrBodyTooLarge   [NEW]
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
 └─ encodeField(rv, key, vals, depth)  [depth threaded everywhere]
      ├─ File -> ErrFileNotSupported (url.Values path)
      ├─ custom converter
      ├─ time.Time (layout)  BEFORE TextMarshaler
      ├─ TextMarshaler (if enabled)
      ├─ struct -> encodeStruct(depth+1)
      ├─ slice/array -> encodeSlice  (key[i])
      ├─ map        -> encodeMap     (key[k])
      └─ scalar     -> formatScalar (vals.Add)
  (multipart twin: encodeStructMultipart / encodeFieldMultipart
   writeFilePart for File, writeStringPart for the rest)

Depth guard at every entry -> ErrMaxDepthExceeded (cycle-safe)
```

## Decoder (decoder.go)

```
Decoder.Unmarshal(vals, &v)
Decoder.UnmarshalMultipart(req, &v)   -> UnmarshalMultipartForm
Decoder.UnmarshalMultipartForm(mf, &v)

parseKeyPath(key) -> []keyToken{kind: field|index|mapkey}

unmarshalValues(vals, elem, depth)
 └─ buildUnmarshalIndex(type) key:name -> field (flattens anon structs)
     └─ for each submitted key: decodePath(tokens)

decodePath(field, rest[], values, depth)
 ├─ deref pointers (allocate if nil)
 ├─ leaf -> assignLeaf
 │         ├─ struct -> time.Time via converter
 │         ├─ slice/array -> append / positional
 │         └─ scalar -> assignScalarTo
 │                     ├─ TextUnmarshaler (if enabled)
 │                     ├─ custom converter
 │                     └─ parseScalar (strconv)
 ├─ field  -> descend into nested struct
 ├─ index  -> slice/array element
 └─ mapkey -> map entry

unmarshalFiles(mf, elem)   • populate File / []File from multipart parts
 ├─ unmarshalTagNames(sf)  • alias keys (form/json/xml/... + Go name)
 ├─ strict -> unknown file parts = DecodingError (was silently dropped)
 └─ readFile(fh)   [NEW]: maxFileSize checked on fh.Size BEFORE read
     over-limit -> DecodingError{ErrFileTooLarge} (whole input rejected)
     (len(f.Content) check kept as fallback for hand-built FileHeaders)

applyDefaultsAndRequired(elem, submittedKeys, depth)   [after decode]
 ├─ required & missing      -> ErrMissingRequired
 └─ default:v & missing     -> assignScalarTo (scalars only, isDefaultable)
```

## Tag system (tag.go) [i]

```
tagResolver (unexported)
 ├─ priority []string        default: form > json > xml > protobuf
 ├─ marshaledFieldName(sf)   (name, skip)
 ├─ marshalFieldOptions(sf)  tagOptions
 ├─ buildUnmarshalIndex(t)   map key -> structField (flattens anon)
 └─ unmarshalFieldName(t,key)

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
                          overflow checks, descriptive errors
assignScalarTo          • TextUnmarshaler -> converter -> parseScalar
built-in converters     • registered in defaultConfig:
     durationConverter  time.Duration  (ParseDuration)
     ipConverter        net.IP
     urlConverter       url.URL
     timeConverter      time.Time      (WithTimeLayout, before TextMarshaler)
```

## Configuration (options.go)

```
config{ tagPriority, maxDepth(32), maxBodySize(0=∞), maxFileSize(0=∞),
        zeroEmpty, timeLayout(RFC3339), converters, textAware(true), strict }
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
FileFromHeader(*multipart.FileHeader) -> File
FilesFromRequest(*http.Request, field) -> []File
(HTTP-aware ONLY here; core is HTTP-agnostic)
```

## Package docs & examples

```
doc.go        • package contract (types, key format, semantics)
_examples/    • basic | nested | multipart | custom-types  (go run)
examples_test.go • godoc-verified Example* outputs
docs/         • DEVELOPER.md (user guide)  MAINTAINER.md (this)  MINDMAP.md
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
 • scanForFiles visited-map cycle guard
 • Marshal priority vs Unmarshal any-tag asymmetry (intentional)
 • Errors always wrap with Unwrap() for errors.Is/As
```
