package anyform

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"reflect"
	"strconv"
	"time"
)

// Converter provides custom marshal/unmarshal logic for a type.
// Implementations are typically registered via WithCustomConverter.
type Converter interface {
	// Marshal converts a reflect.Value to a string for encoding.
	Marshal(value reflect.Value) (string, error)
	// Unmarshal converts a string into the field's value.
	Unmarshal(value string, field reflect.Value) error
}

// timeConverter handles time.Time using a configurable layout.
type timeConverter struct {
	layout string
}

func (c *timeConverter) Marshal(value reflect.Value) (string, error) {
	if !value.IsValid() {
		return "", nil
	}
	t, ok := value.Interface().(time.Time)
	if !ok {
		return "", &EncodingError{Err: errors.New("anyform: value is not time.Time")}
	}
	return t.Format(c.layout), nil
}

func (c *timeConverter) Unmarshal(value string, field reflect.Value) error {
	if value == "" {
		return nil
	}
	t, err := time.Parse(c.layout, value)
	if err != nil {
		return fmt.Errorf("parsing time %q with layout %q: %w", value, c.layout, err)
	}
	if !field.CanSet() {
		return errors.New("cannot set time.Time field")
	}
	field.Set(reflect.ValueOf(t))
	return nil
}

// durationConverter handles time.Duration as a duration string (e.g. "5s", "1h30m").
type durationConverter struct{}

func (durationConverter) Marshal(value reflect.Value) (string, error) {
	if !value.IsValid() {
		return "", nil
	}
	if d, ok := value.Interface().(time.Duration); ok {
		if d == 0 {
			return "", nil
		}
		return d.String(), nil
	}
	return "", &EncodingError{Err: errors.New("anyform: value is not time.Duration")}
}

func (durationConverter) Unmarshal(value string, field reflect.Value) error {
	if value == "" {
		return nil
	}
	d, err := time.ParseDuration(value)
	if err != nil {
		return fmt.Errorf("parsing duration %q: %w", value, err)
	}
	field.Set(reflect.ValueOf(d))
	return nil
}

// ipConverter handles net.IP as a string.
type ipConverter struct{}

func (ipConverter) Marshal(value reflect.Value) (string, error) {
	if !value.IsValid() {
		return "", nil
	}
	if ip, ok := value.Interface().(net.IP); ok {
		if ip == nil {
			return "", nil
		}
		return ip.String(), nil
	}
	return "", &EncodingError{Err: errors.New("anyform: value is not net.IP")}
}

func (ipConverter) Unmarshal(value string, field reflect.Value) error {
	if value == "" {
		field.Set(reflect.ValueOf(net.IP(nil)))
		return nil
	}
	ip := net.ParseIP(value)
	if ip == nil {
		return fmt.Errorf("parsing IP %q: invalid address", value)
	}
	field.Set(reflect.ValueOf(ip))
	return nil
}

// urlConverter handles url.URL as a string.
type urlConverter struct{}

func (urlConverter) Marshal(value reflect.Value) (string, error) {
	if !value.IsValid() {
		return "", nil
	}
	if u, ok := value.Interface().(url.URL); ok {
		return u.String(), nil
	}
	return "", &EncodingError{Err: errors.New("anyform: value is not url.URL")}
}

func (urlConverter) Unmarshal(value string, field reflect.Value) error {
	if value == "" {
		return nil
	}
	u, err := url.Parse(value)
	if err != nil {
		return fmt.Errorf("parsing URL %q: %w", value, err)
	}
	field.Set(reflect.ValueOf(*u))
	return nil
}

// formatScalar converts a scalar value to its string representation.
func formatScalar(value reflect.Value) string {
	switch value.Kind() {
	case reflect.Bool:
		if value.Bool() {
			return "true"
		}
		return "false"
	case reflect.String:
		return value.String()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(value.Int(), 10)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return strconv.FormatUint(value.Uint(), 10)
	case reflect.Float32:
		return strconv.FormatFloat(value.Float(), 'g', -1, 32)
	case reflect.Float64:
		return strconv.FormatFloat(value.Float(), 'g', -1, 64)
	case reflect.Complex64:
		return strconv.FormatComplex(value.Complex(), 'g', -1, 64)
	case reflect.Complex128:
		return strconv.FormatComplex(value.Complex(), 'g', -1, 128)
	default:
		return fmt.Sprint(value.Interface())
	}
}

// parseScalar assigns a string to a scalar field's value, parsing it according
// to the field's kind.
func parseScalar(s string, field reflect.Value) error {
	switch field.Kind() {
	case reflect.Bool:
		b, err := strconv.ParseBool(s)
		if err != nil {
			return fmt.Errorf("parsing bool %q: %w", s, err)
		}
		field.SetBool(b)
	case reflect.String:
		field.SetString(s)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return fmt.Errorf("parsing int %q: %w", s, err)
		}
		if field.OverflowInt(v) {
			return fmt.Errorf("int %q overflows %s", s, field.Type())
		}
		field.SetInt(v)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		v, err := strconv.ParseUint(s, 10, 64)
		if err != nil {
			return fmt.Errorf("parsing uint %q: %w", s, err)
		}
		if field.OverflowUint(v) {
			return fmt.Errorf("uint %q overflows %s", s, field.Type())
		}
		field.SetUint(v)
	case reflect.Float32, reflect.Float64:
		v, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return fmt.Errorf("parsing float %q: %w", s, err)
		}
		field.SetFloat(v)
	case reflect.Complex64, reflect.Complex128:
		v, err := strconv.ParseComplex(s, 128)
		if err != nil {
			return fmt.Errorf("parsing complex %q: %w", s, err)
		}
		field.SetComplex(v)
	default:
		return fmt.Errorf("unsupported field kind %s", field.Kind())
	}
	return nil
}
