package core

import (
	"errors"
	"fmt"
)

type ErrModuleUnavailable struct {
	Name string
}

func (err ErrModuleUnavailable) Error() string {
	return fmt.Sprintf("module %s unavailable", err.Name)
}

func errModuleUnavailable(name string) ErrModuleUnavailable {
	return ErrModuleUnavailable{Name: name}
}

// Is matches any ErrModuleUnavailable regardless of module name.
func (ErrModuleUnavailable) Is(other error) bool {
	var e ErrModuleUnavailable
	return errors.As(other, &e)
}
