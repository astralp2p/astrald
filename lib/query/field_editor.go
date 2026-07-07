package query

import (
	"encoding"
	"encoding/base64"
	"errors"
	"reflect"
	"strconv"

	"github.com/cryptopunkscc/astral-go/astral"
)

// FieldEditor wraps a struct field and provides an editing interface
type FieldEditor struct {
	field reflect.Value
	tag   *FieldTag
}

// astral:blueprint-ignore
func (editor FieldEditor) ObjectType() string {
	// check if an astral.Object first
	if typed, ok := editor.field.Interface().(astral.Object); ok {
		if editor.field.Kind() == reflect.Pointer && editor.field.IsNil() {
			typed = reflect.New(editor.field.Type().Elem()).Interface().(astral.Object)
		}
		return typed.ObjectType()
	}
	if editor.field.CanAddr() {
		typed, ok := editor.field.Addr().Interface().(astral.Object)
		if ok {
			return typed.ObjectType()
		}
	}

	f := editor.field.Type()
	if f.Kind() == reflect.Ptr {
		f = f.Elem()
	}

	switch f.Kind() {
	case reflect.String:
		return astral.String8("").ObjectType()
	case reflect.Int8:
		return astral.Int8(0).ObjectType()
	case reflect.Int16:
		return astral.Int16(0).ObjectType()
	case reflect.Int32:
		return astral.Int32(0).ObjectType()
	case reflect.Int64, reflect.Int:
		return astral.Int64(0).ObjectType()
	case reflect.Uint8:
		return astral.Uint8(0).ObjectType()
	case reflect.Uint16:
		return astral.Uint16(0).ObjectType()
	case reflect.Uint32:
		return astral.Uint32(0).ObjectType()
	case reflect.Uint64, reflect.Uint:
		return astral.Uint64(0).ObjectType()
	case reflect.Float32:
		return astral.Float32(0).ObjectType()
	case reflect.Float64:
		return astral.Float32(0).ObjectType()
	case reflect.Bool:
		return astral.Bool(false).ObjectType()

	case reflect.Slice:
		if f.Elem().Kind() == reflect.Uint8 {
			return astral.Bytes32{}.ObjectType()
		}
	}

	return ""
}

// newFieldEditor returns a FieldEditor for the given struct field
func newFieldEditor(field reflect.Value, typ reflect.StructField) *FieldEditor {
	lookup, _ := typ.Tag.Lookup(queryTag)

	return &FieldEditor{field: field, tag: ParseTag(lookup)}
}

func (editor FieldEditor) Get() string {
	text, _ := editor.MarshalText()
	return string(text)
}

func (editor FieldEditor) Set(value string) error {
	return editor.UnmarshalText([]byte(value))
}

func (editor FieldEditor) Tag() *FieldTag {
	return editor.tag
}

// text

func (editor FieldEditor) UnmarshalText(data []byte) error {
	f := editor.field
	if f.Kind() == reflect.Ptr {
		if f.IsNil() {
			f.Set(reflect.New(f.Type().Elem()))
		}
		f = f.Elem()
	}

	// check explicit text unmarshaler first
	if u, ok := editor.field.Interface().(encoding.TextUnmarshaler); ok {
		return u.UnmarshalText(data)
	}

	if editor.field.CanAddr() {
		if u, ok := editor.field.Addr().Interface().(encoding.TextUnmarshaler); ok {
			return u.UnmarshalText(data)
		}
	}

	// handle native types
	switch f.Kind() {
	case reflect.String:
		f.SetString(string(data))

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := strconv.ParseInt(string(data), 10, 64)
		if err != nil {
			return err
		}
		f.SetInt(n)

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n, err := strconv.ParseUint(string(data), 10, 64)
		if err != nil {
			return err
		}
		f.SetUint(n)

	case reflect.Float32, reflect.Float64:
		n, err := strconv.ParseFloat(string(data), 64)
		if err != nil {
			return err
		}
		f.SetFloat(n)

	case reflect.Bool:
		n, err := strconv.ParseBool(string(data))
		if err != nil {
			return err
		}
		f.SetBool(n)

	case reflect.Slice:
		if f.Type().Elem().Kind() == reflect.Uint8 {
			b, err := base64.StdEncoding.DecodeString(string(data))
			if err != nil {
				return err
			}
			f.Set(reflect.ValueOf(b))
		}
	default:
		return errors.New("field does not implement TextUnmarshaler")
	}

	return nil
}

func (editor FieldEditor) MarshalText() ([]byte, error) {
	if editor.field.Kind() == reflect.Ptr && editor.field.IsNil() {
		return nil, nil
	}

	// check explicit marshalers first
	if typed, ok := editor.field.Interface().(encoding.TextMarshaler); ok {
		return typed.MarshalText()
	}
	if editor.field.CanAddr() {
		typed, ok := editor.field.Addr().Interface().(encoding.TextMarshaler)
		if ok {
			return typed.MarshalText()
		}
	}

	f := editor.field
	if f.Kind() == reflect.Ptr {
		f = f.Elem()
	}

	switch f.Kind() {
	case reflect.String:
		return []byte(f.String()), nil

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return []byte(strconv.FormatInt(f.Int(), 10)), nil

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return []byte(strconv.FormatUint(f.Uint(), 10)), nil

	case reflect.Float32, reflect.Float64:
		return []byte(strconv.FormatFloat(f.Float(), 'g', -1, 64)), nil

	case reflect.Bool:
		return []byte(strconv.FormatBool(f.Bool())), nil

	case reflect.Slice:
		if f.Type().Elem().Kind() == reflect.Uint8 {
			return []byte(base64.StdEncoding.EncodeToString(f.Interface().([]byte))), nil
		}
	}

	return nil, errors.New("field does not implement TextMarshaler")
}

func (editor FieldEditor) String() string {
	text, _ := editor.MarshalText()
	return string(text)
}
