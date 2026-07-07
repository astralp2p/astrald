package tree

import (
	"errors"
	"fmt"
	"github.com/cryptopunkscc/astral-go/api/tree"
	"reflect"
	"strings"

	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/log"
)

// Bind binds Value fields in struct s to the node
func Bind(ctx *astral.Context, s any, node tree.Node) error {
	var v = reflect.ValueOf(s)

	if v.Kind() != reflect.Ptr {
		return errors.New("s must be a pointer to a struct")
	}
	v = v.Elem()
	if v.Kind() != reflect.Struct {
		return errors.New("s must be a pointer to a struct")
	}

	for i := range v.NumField() {
		field := v.Field(i)
		fieldType := v.Type().Field(i)

		if !field.CanInterface() {
			// skip unexported fields
			continue
		}

		if field.Kind() == reflect.Pointer {
			if field.IsNil() {
				field.Set(reflect.New(field.Type().Elem()))
			}
			field = field.Elem()
		}

		if field.Kind() != reflect.Struct {
			continue
		}

		// get the tag
		tag := parseTag(fieldType.Tag.Get("tree"))
		if tag.skip {
			continue
		}

		// get the key name
		keyName := log.ToSnakeCase(fieldType.Name)
		if tag.path != "" {
			keyName = tag.path
		}

		// find the bind method
		bind, found := findBindMethod(field)
		if found {
			fieldNode, err := tree.Query(ctx, node, keyName, true)
			if err != nil {
				return err
			}

			ret := bind.Call([]reflect.Value{reflect.ValueOf(ctx), reflect.ValueOf(fieldNode)})

			if !ret[0].IsNil() {
				err := ret[0].Interface().(error)
				return fmt.Errorf("failed to bind field %s to key %s: %w", fieldType.Name, keyName, err)
			}
			continue
		}

		subNode, err := tree.Query(ctx, node, keyName, true)
		if err != nil {
			return err
		}

		err = Bind(ctx, field.Addr().Interface(), subNode)
		if err != nil {
			return err
		}
	}

	return nil
}

// BindPath is a convenience function that queries the node and calls Bind.
func BindPath(ctx *astral.Context, s any, node tree.Node, path string, create bool) (err error) {
	node, err = tree.Query(ctx, node, path, create)
	if err != nil {
		return err
	}

	return Bind(ctx, s, node)
}

func findBindMethod(field reflect.Value) (reflect.Value, bool) {
	if field.Kind() == reflect.Struct {
		field = field.Addr()
	}

	for j := range field.NumMethod() {
		mType := field.Type().Method(j)

		// check method signature
		switch {
		case mType.Name != "Bind":
			continue
		case mType.Type.NumIn() != 3:
			continue
		case mType.Type.In(1).Kind() != reflect.Pointer:
			continue
		case mType.Type.In(1).Elem() != reflect.TypeOf((*astral.Context)(nil)).Elem():
			continue
		case mType.Type.In(2) != reflect.TypeOf((*tree.Node)(nil)).Elem():
			continue
		case mType.Type.NumOut() != 1:
			continue
		case mType.Type.Out(0) != reflect.TypeOf((*error)(nil)).Elem():
			continue
		}

		return field.Method(j), true
	}

	return reflect.Value{}, false
}

func parseTag(s string) (tag tag) {
	elems := strings.Split(s, ";")
	if len(elems) == 0 {
		return
	}

	tag.path = elems[0]
	elems = elems[1:]

	for _, elem := range elems {
		switch elem {
		case "skip":
			tag.skip = true
		}
	}
	return
}

type tag struct {
	path string
	skip bool
}
