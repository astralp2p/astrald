package resources

import (
	"github.com/astralp2p/astral-go/sig"
)

var _ Resources = &MemResources{}

type MemResources struct {
	res sig.Map[string, []byte]
}

func NewMemResources() *MemResources {
	return &MemResources{}
}

func (res *MemResources) Read(name string) ([]byte, error) {
	bytes, ok := res.res.Get(name)
	if !ok {
		return nil, ErrNotFound
	}

	return bytes, nil
}

func (res *MemResources) Write(name string, data []byte) error {
	res.res.Replace(name, data)

	return nil
}
