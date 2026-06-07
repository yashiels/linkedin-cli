// Package restli implements LinkedIn's custom RestLi encoding for GraphQL variables.
//
// LinkedIn does not use standard JSON or URL-encoded parameters for GraphQL variable
// payloads. Instead it uses a custom wire format where:
//
//	Objects     → (key:value,key2:value2)
//	Nested objs → (outer:(inner:value))
//	Lists       → List(item1,item2,item3)
//	Strings     → URL-percent-encoded (spaces → %20, colons in URNs NOT pre-encoded)
//	Booleans    → true / false (lowercase)
//	Empty str   → '' (two single-quotes)
package restli

import (
	"fmt"
	"net/url"
	"reflect"
	"sort"
	"strings"
)

// Encoder converts Go values into the LinkedIn RestLi wire format.
type Encoder struct {
	// SortKeys makes map key ordering deterministic (useful for testing).
	SortKeys bool
}

// Default is a ready-to-use encoder with stable key ordering.
var Default = &Encoder{SortKeys: true}

// Encode converts v into RestLi format.
// v may be a map, struct, slice, array, bool, string, or numeric value.
func Encode(v interface{}) (string, error) {
	return Default.Encode(v)
}

// Encode converts v into RestLi format.
func (e *Encoder) Encode(v interface{}) (string, error) {
	if v == nil {
		return "null", nil
	}
	return e.encodeValue(reflect.ValueOf(v))
}

func (e *Encoder) encodeValue(v reflect.Value) (string, error) {
	// Dereference pointers/interfaces.
	for v.Kind() == reflect.Ptr || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return "null", nil
		}
		v = v.Elem()
	}

	switch v.Kind() {
	case reflect.Bool:
		if v.Bool() {
			return "true", nil
		}
		return "false", nil

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return fmt.Sprintf("%d", v.Int()), nil

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return fmt.Sprintf("%d", v.Uint()), nil

	case reflect.Float32, reflect.Float64:
		return fmt.Sprintf("%g", v.Float()), nil

	case reflect.String:
		return encodeString(v.String()), nil

	case reflect.Slice, reflect.Array:
		return e.encodeList(v)

	case reflect.Map:
		return e.encodeMap(v)

	case reflect.Struct:
		return e.encodeStruct(v)

	default:
		return "", fmt.Errorf("restli: unsupported type %s", v.Type())
	}
}

// encodeString URL-percent-encodes a string value.
// Empty strings become ” per the LinkedIn wire format.
func encodeString(s string) string {
	if s == "" {
		return "''"
	}
	// url.PathEscape encodes spaces as %20, keeps / unencoded.
	// We want spaces → %20, colons → literal (URNs keep their colons inside
	// the RestLi value; they are only re-encoded at the outer URL query level).
	encoded := url.QueryEscape(s)
	// QueryEscape turns spaces into '+'. Normalise to %20.
	encoded = strings.ReplaceAll(encoded, "+", "%20")
	return encoded
}

func (e *Encoder) encodeList(v reflect.Value) (string, error) {
	if v.IsNil() || v.Len() == 0 {
		return "List()", nil
	}
	parts := make([]string, 0, v.Len())
	for i := 0; i < v.Len(); i++ {
		s, err := e.encodeValue(v.Index(i))
		if err != nil {
			return "", err
		}
		parts = append(parts, s)
	}
	return "List(" + strings.Join(parts, ",") + ")", nil
}

func (e *Encoder) encodeMap(v reflect.Value) (string, error) {
	if v.IsNil() || v.Len() == 0 {
		return "()", nil
	}
	keys := v.MapKeys()
	if e.SortKeys {
		sort.Slice(keys, func(i, j int) bool {
			return keys[i].String() < keys[j].String()
		})
	}
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		keyStr := k.String()
		valStr, err := e.encodeValue(v.MapIndex(k))
		if err != nil {
			return "", err
		}
		parts = append(parts, keyStr+":"+valStr)
	}
	return "(" + strings.Join(parts, ",") + ")", nil
}

// structTag retrieves the restli tag name for a field, falling back to the
// lowercase field name.
func structTag(f reflect.StructField) (string, bool) {
	tag := f.Tag.Get("restli")
	if tag == "" {
		tag = f.Tag.Get("json")
	}
	if tag == "" {
		return strings.ToLower(f.Name), false
	}
	parts := strings.Split(tag, ",")
	name := parts[0]
	if name == "-" {
		return "", true // skip
	}
	if name == "" {
		name = strings.ToLower(f.Name)
	}
	return name, false
}

func (e *Encoder) encodeStruct(v reflect.Value) (string, error) {
	t := v.Type()
	type kv struct{ k, v string }
	var fields []kv
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		name, skip := structTag(f)
		if skip {
			continue
		}
		fv := v.Field(i)
		// Skip zero values (like JSON omitempty logic).
		if fv.IsZero() {
			continue
		}
		valStr, err := e.encodeValue(fv)
		if err != nil {
			return "", err
		}
		fields = append(fields, kv{name, valStr})
	}
	if len(fields) == 0 {
		return "()", nil
	}
	if e.SortKeys {
		sort.Slice(fields, func(i, j int) bool { return fields[i].k < fields[j].k })
	}
	parts := make([]string, len(fields))
	for i, f := range fields {
		parts[i] = f.k + ":" + f.v
	}
	return "(" + strings.Join(parts, ",") + ")", nil
}

// MustEncode encodes v or panics. Useful in tests and static initialisers.
func MustEncode(v interface{}) string {
	s, err := Encode(v)
	if err != nil {
		panic(err)
	}
	return s
}
