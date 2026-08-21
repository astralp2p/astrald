package assets

import (
	"github.com/astralp2p/astrald/resources"
	"gorm.io/gorm"
)

// Assets is the contract for node-local storage: raw resources, YAML config, and a shared database handle.
type Assets interface {
	Res() resources.Resources
	Read(name string) ([]byte, error)
	Write(name string, data []byte) error
	LoadYAML(name string, out interface{}) error
	StoreYAML(name string, in interface{}) error
	Database() *gorm.DB
}
