// Package anyform provides seamless marshalling and unmarshalling of Go structs
// to and from HTML form data, URL-encoded values, and multipart form data.
//
// Unlike existing form packages, anyform supports:
//   - A configurable tag priority system (form > json > xml > protobuf)
//   - Native file upload handling via the [File] type
//   - All Go types as field values, including maps, nested structs, slices,
//     and custom types
//   - Both a simple top-level API and a configurable Encoder/Decoder API
//   - Zero external dependencies
//
// # Quick Start
//
// The top-level [Marshal] and [Unmarshal] functions do all the work behind the
// scenes. [Marshal] returns a body together with its Content-Type, choosing
// multipart/form-data automatically when the struct contains [File] fields and
// application/x-www-form-urlencoded otherwise. [Unmarshal] decodes a body back
// into a struct, auto-detecting the format from the Content-Type.
//
//	type User struct {
//	    Name  string `form:"name"`
//	    Email string `form:"email"`
//	}
//
//	body, ct, err := anyform.Marshal(User{Name: "Alice", Email: "alice@example.com"})
//	// body == "email=alice%40example.com&name=Alice", ct == "application/x-www-form-urlencoded"
//
//	var user User
//	err := anyform.Unmarshal(body, ct, &user)
//
// # Encoder / Decoder
//
// For reusable, pre-configured instances (custom converters, time layouts, tag
// priorities), use [NewEncoder], [Encoder.Marshal], [NewDecoder], and
// [Decoder.Unmarshal]. They are thread-safe and accept the same functional
// options as the top-level functions.
//
// # Tag Priority
//
// anyform resolves struct field names using a configurable tag priority.
// The default order is: form > json > xml > protobuf. If a tag is not found,
// the exported Go field name is used as a fallback.
//
// During unmarshalling, the submitted key is matched against all available
// tags, so clients can use any supported tag name.
//
// # Tag Options
//
// Tags support ,omitempty, ,required, and ,default:value:
//
//	type Config struct {
//	    Region string `form:"region,default:us-east"`
//	    Token  string `form:"token,required"`
//	}
//
// required and default are enforced during unmarshalling against the field's
// resolved form name.
//
// # File Uploads
//
// Fields of type [File] or [File] are populated from multipart form data:
//
//	type Upload struct {
//	    Avatar anyform.File   `form:"avatar"`
//	    Docs   anyform.Files  `form:"documents"`
//	}
//
// The [File] type holds the raw content, detected content type, and original
// filename, decoupled from net/http so it works in any context. Note that file
// parts should carry a filename for reliable multipart detection. A file part
// that is present but carries zero bytes is still bound: the resulting [File]
// has empty Content but preserves its Filename. A part with an empty filename
// is classified as a value field by the multipart parser.
//
// # Supported types
//
// [Marshal] and [Unmarshal] handle every common Go type. The value passed in
// must always be a struct (or pointer to a struct): slices, arrays, maps, and
// primitives are supported only as field values, since every form key names a
// field — a bare root slice would have no namespace to attach its keys to.
//
//   - Scalars: string, bool, all int/uint/float/complex widths (via strconv)
//   - Named types: type MyInt int, type Status string, etc. (via reflection)
//   - Pointers: *T (nil is omitted on marshal; allocated on unmarshal)
//   - Slices and arrays: []T, [N]T — encoded as repeated indexed keys
//   - Maps: map[K]V — encoded as bracket-named keys
//   - Structs: nested and embedded (anonymous) structs are flattened
//   - time.Time (RFC3339 by default, configurable via WithTimeLayout)
//   - time.Duration and net.IP / url.URL (built-in converters)
//   - Types implementing encoding.TextMarshaler / TextUnmarshaler
//   - File and []File across multipart round-trips
//
// # Key format
//
// The form keys produced and accepted follow this convention:
//
//	"name"          -> field name
//	"a.b"           -> nested struct field
//	"items[0]"      -> slice/array index
//	"items[0].name" -> index, then a field
//	"attr[key]"     -> map key
//	"matrix[0][1]"  -> nested indexes
//
// Repeating a scalar key ("k=a&k=b") yields a []string / slice on unmarshal.
//
// # Marshal & Unmarshal semantics
//
//   - Tag skip semantics: a field is omitted when the first tag in priority
//     order is "-". A json:"-" with an earlier form tag still participates.
//   - ,omitempty and the global WithZeroEmpty omit empty values on marshal.
//     Zero values are otherwise emitted, including zero time.Time values
//     (formatted per the configured layout).
//   - ,required produces ErrMissingRequired when the field is absent.
//   - ,default:v populates an absent field with v (scalar kinds only).
//   - ,strict (WithStrictUnmarshal) reports unknown keys as errors. This covers
//     both value fields and multipart file parts.
//   - Ambiguous keys are rejected: when two different fields resolve to the
//     same key (an embedded promoted field and an outer field sharing a tag,
//     or two sibling fields sharing a tag), unmarshal returns a DecodingError
//     for that key instead of silently routing one field's payload into the
//     other. This applies to value keys and multipart file parts alike; a
//     colliding file part would otherwise be consumed by every matching File
//     field. Strict mode is not required.
//   - Every decode failure is a *DecodingError, so errors.As(err,
//     &*DecodingError{}) always succeeds — even for plain scalar parse
//     failures (int overflow, bad bool), which previously escaped as bare
//     errors.
//   - WithMaxBodySize limits the whole body passed to Unmarshal
//     (ErrBodyTooLarge); WithMaxFileSize limits each file part
//     (ErrFileTooLarge). Both are 0 (= unlimited) by default. A file part
//     exceeding the file limit is rejected using its declared size before the
//     content is read into memory.
//   - Unmarshalling matches a submitted key against any tag name in the
//     priority list, not just the primary form name. This applies to value
//     fields AND File/[]File fields: a file part named by any of the field's
//     tags is accepted.
package anyform
