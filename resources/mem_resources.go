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

// ReadData reads name.
// note: memory resources have one namespace, so a data name and a config name share one map
func (res *MemResources) ReadData(name string) ([]byte, error) {
	return res.Read(name)
}

// WriteData writes data to name.
func (res *MemResources) WriteData(name string, data []byte) error {
	return res.Write(name, data)
}
