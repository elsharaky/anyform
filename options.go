package anyform

import (
	"net"
	"net/url"
	"reflect"
	"time"
) // defaultMaxDepth is the default maximum nested struct depth.
const defaultMaxDepth = 32

// Option configures an Encoder or Decoder.
type Option func(*config)

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

// defaultConfig returns the default configuration.
func defaultConfig() *config {
	return &config{
		tagPriority: defaultTagPriority,
		maxDepth:    defaultMaxDepth,
		timeLayout:  time.RFC3339,
		converters: map[reflect.Type]Converter{
			reflect.TypeOf(time.Duration(0)): durationConverter{},
			reflect.TypeOf(net.IP{}):         ipConverter{},
			reflect.TypeOf(url.URL{}):        urlConverter{},
		},
		textAware: true,
	}
}

// WithTagPriority sets the tag resolution order for encoding.
// Default: form, json, xml, protobuf.
func WithTagPriority(tags ...string) Option {
	return func(c *config) {
		if len(tags) > 0 {
			c.tagPriority = tags
		}
	}
}

// WithMaxDepth sets the maximum nested struct depth. Returns ErrMaxDepthExceeded
// when exceeded. Default: 32.
func WithMaxDepth(n int) Option {
	return func(c *config) {
		if n > 0 {
			c.maxDepth = n
		}
	}
}

// WithZeroEmpty controls whether zero-valued fields are omitted during marshalling.
// When true, fields whose value is empty (zero scalars, nil pointers, empty
// slices/maps/strings) are not emitted, as if every field carried omitempty.
// When false (default), zero values are emitted explicitly (e.g. "0", "").
// This is a global default; per-field omitempty tags always take precedence.
func WithZeroEmpty(zero bool) Option {
	return func(c *config) {
		c.zeroEmpty = zero
	}
}

// WithTimeLayout sets the layout used to marshal/unmarshal time.Time values.
// Default: time.RFC3339.
func WithTimeLayout(layout string) Option {
	return func(c *config) {
		if layout != "" {
			c.timeLayout = layout
		}
	}
}

// WithCustomConverter registers a converter for a specific type, overriding
// the default behavior for that type during both marshal and unmarshal.
// The converter type must match the reflect.Type exactly (not its alias).
func WithCustomConverter(t reflect.Type, conv Converter) Option {
	return func(c *config) {
		if t != nil && conv != nil {
			c.converters[t] = conv
		}
	}
}

// WithTextMarshalerSupport enables automatic use of encoding.TextMarshaler and
// encoding.TextUnmarshaler for types that implement them. Default: true.
func WithTextMarshalerSupport(enabled bool) Option {
	return func(c *config) {
		c.textAware = enabled
	}
}

// WithStrictUnmarshal controls whether an unknown submitted key produces an error.
// When false (default), unknown keys are ignored. When true, an error is returned.
func WithStrictUnmarshal(strict bool) Option {
	return func(c *config) {
		c.strict = strict
	}
}

// WithMaxFileSize sets the maximum permitted size, in bytes, of each individual
// file part during unmarshalling. A file larger than this limit causes the
// operation to fail with ErrFileTooLarge.
// A value <= 0 disables the limit (the default).
func WithMaxFileSize(limit int64) Option {
	return func(c *config) {
		c.maxFileSize = limit
	}
}

// WithMaxBodySize sets the maximum permitted size, in bytes, of the raw body
// passed to Unmarshal. A body larger than this limit fails with
// ErrBodyTooLarge before parsing.
// A value <= 0 disables the limit (the default).
func WithMaxBodySize(limit int64) Option {
	return func(c *config) {
		c.maxBodySize = limit
	}
}
