package query

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"strings"

	"github.com/cryptopunkscc/astral-go/astral/log"
)

var ErrFieldNotFound = errors.New("field not found")

// Editor wraps a struct and provides getter/setters for the fields
type Editor struct {
	arg    reflect.Value // value of the struct (dereferenced)
	fields []*field
}

type field struct {
	name string
	*FieldEditor
}

// FieldSpec describes a field in the Editor struct
type FieldSpec struct {
	Name     string
	Type     string
	Required bool
}

// Edit returns an Editor for s, which must be a pointer to a struct, otherwise Edit will panic.
func Edit(s any) *Editor {
	return edit(s, false)
}

// EditCamel returns an Editor for s, preserving the original field name casing
// instead of converting to snake_case as Edit does.
func EditCamel(args any) *Editor {
	return edit(args, true)
}

// EditValue returns an Editor from an already-obtained reflect.Value; useful
// when the caller already holds a reflected struct pointer.
func EditValue(v reflect.Value) *Editor {
	return editValue(v, false)
}

func edit(args any, keepCase bool) *Editor {
	return editValue(reflect.ValueOf(args), keepCase)
}

func editValue(v reflect.Value, keepCase bool) *Editor {
	// make sure arg is a pointer to a struct so we can modify it
	if v.Kind() == reflect.Ptr && v.Elem().Kind() == reflect.Ptr {
		if v.Elem().IsZero() {
			v.Elem().Set(reflect.New(v.Type().Elem().Elem()))
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Ptr || v.IsZero() || v.Elem().Kind() != reflect.Struct {
		panic("invalid argument: argument must be a pointer to a struct")
	}
	v = v.Elem()

	editor := &Editor{
		arg: v,
	}

	for i := 0; i < v.NumField(); i++ {
		fv := v.Field(i)
		ft := v.Type().Field(i)

		if !fv.CanInterface() {
			continue
		}

		fieldEditor := newFieldEditor(fv, ft)

		// don't include fields with the skip tag
		if fieldEditor.Tag().Skip {
			continue
		}

		name := ft.Name

		if !keepCase {
			name = log.ToSnakeCase(name)
		}

		if fieldEditor.Tag().Key != "" {
			name = fieldEditor.Tag().Key
		}

		editor.fields = append(editor.fields, &field{
			name:        name,
			FieldEditor: fieldEditor,
		})
	}

	return editor
}

func (editor *Editor) Set(name string, value string) error {
	field, err := editor.Field(name)
	if err != nil {
		return fmt.Errorf("cannot set %s: %w", name, err)
	}

	return field.Set(value)
}

func (editor *Editor) Get(name string) (string, error) {
	field, err := editor.Field(name)
	if err != nil {
		return "", err
	}

	return field.Get(), nil
}

// Spec returns a map containing specs for every argument
func (editor *Editor) Spec() (vals []FieldSpec) {
	for _, editor := range editor.fields {
		objectType := editor.ObjectType()
		if objectType == "" {
			continue
		}
		vals = append(vals, FieldSpec{
			Name:     editor.name,
			Type:     objectType,
			Required: editor.Tag().Required,
		})
	}
	return
}

func (editor *Editor) Field(name string) (*FieldEditor, error) {
	for _, field := range editor.fields {
		if field.name == name {
			return field.FieldEditor, nil
		}
	}

	return nil, ErrFieldNotFound
}

// SetMany applies a map of values to the editor's fields, silently skipping
// unknown keys; returns an error only on type-conversion failures.
func (editor *Editor) SetMany(vals map[string]string) error {
	for key, value := range vals {
		err := editor.Set(key, value)
		switch {
		case err == nil:
		case errors.Is(err, ErrFieldNotFound):
			continue
		default:
			return fmt.Errorf("cannot set %s: %w", key, err)
		}
	}
	return nil
}

// SetArgs parses a "-key value" argument slice into the editor's fields,
// returning unconsumed (non-flag) args and an error for any unknown flag.
func (editor *Editor) SetArgs(args []string) (unparsed []string, err error) {
	var i = 0

	for i < len(args) {
		argName, found := strings.CutPrefix(args[i], "-")
		if !found {
			unparsed = append(unparsed, args[i])
			continue
		}

		if i+1 >= len(args) {
			unparsed = append(unparsed, args[i])
			return
		}

		err = editor.Set(argName, args[i+1])
		switch {
		case err == nil:
			i += 2
		case errors.Is(err, ErrFieldNotFound):
			return unparsed, fmt.Errorf("unknown argument: %s", argName)
		default:
			return
		}
	}

	return
}

// json (passthrough)

func (editor *Editor) MarshalJSON() ([]byte, error) {
	return json.Marshal(editor.arg.Interface())
}

func (editor *Editor) UnmarshalJSON(bytes []byte) error {
	return json.Unmarshal(bytes, editor.arg.Addr().Interface())
}

// query

func (editor *Editor) MarshalQuery() ([]byte, error) {
	var vals = url.Values{}

	for _, field := range editor.fields {
		text, err := field.MarshalText()
		if err != nil {
			return nil, err
		}
		if text == nil {
			continue
		}

		vals.Set(field.name, string(text))
	}

	return []byte(vals.Encode()), nil
}

func (editor *Editor) UnmarshalQuery(text []byte) error {
	vals, err := url.ParseQuery(string(text))
	if err != nil {
		return err
	}

	for key, value := range vals {
		err = editor.Set(key, value[0])
		if err != nil {
			return err
		}
	}
	return nil
}

func (editor *Editor) String() string {
	text, _ := editor.MarshalQuery()
	return string(text)
}
