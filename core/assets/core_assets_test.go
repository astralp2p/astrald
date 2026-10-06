package assets

import (
	"errors"
	"testing"

	"github.com/astralp2p/astrald/resources"
)

type yamlConfig struct {
	A int `yaml:"a"`
}

func TestCoreAssetsYAMLRoundTrip(t *testing.T) {
	res := resources.NewMemResources()
	a := &CoreAssets{res: res}

	if err := a.StoreYAML("node", yamlConfig{A: 7}); err != nil {
		t.Fatalf("StoreYAML: %v", err)
	}
	data, err := res.Read("node.yaml")
	if err != nil || string(data) != "a: 7\n" {
		t.Fatalf("resource node.yaml = (%q, %v), want (%q, nil)", data, err, "a: 7\n")
	}

	var out yamlConfig
	if err := a.LoadYAML("node", &out); err != nil {
		t.Fatalf("LoadYAML: %v", err)
	}
	if out.A != 7 {
		t.Fatalf("LoadYAML A = %d, want 7", out.A)
	}
}

func TestCoreAssetsStoreYAMLKeepsSuffix(t *testing.T) {
	res := resources.NewMemResources()
	a := &CoreAssets{res: res}

	if err := a.StoreYAML("x.YAML", yamlConfig{A: 1}); err != nil {
		t.Fatalf("StoreYAML: %v", err)
	}
	if _, err := res.Read("x.YAML"); err != nil {
		t.Fatalf("resource x.YAML: %v, want stored under the given name", err)
	}
	if _, err := res.Read("x.YAML.yaml"); !errors.Is(err, resources.ErrNotFound) {
		t.Fatalf("resource x.YAML.yaml error = %v, want %v", err, resources.ErrNotFound)
	}
}

func TestCoreAssetsLoadYAMLMissing(t *testing.T) {
	a := &CoreAssets{res: resources.NewMemResources()}

	var out yamlConfig
	if err := a.LoadYAML("missing", &out); !errors.Is(err, resources.ErrNotFound) {
		t.Fatalf("LoadYAML(missing) error = %v, want %v", err, resources.ErrNotFound)
	}
}

type plainResources struct{}

func (plainResources) Read(string) ([]byte, error) { return nil, resources.ErrNotFound }
func (plainResources) Write(string, []byte) error  { return nil }

func TestCoreAssetsOpenDatabaseErrors(t *testing.T) {
	tests := []struct {
		name   string
		res    resources.Resources
		dbName string
		want   string
	}{
		{name: "empty name", res: resources.NewMemResources(), dbName: "", want: "invalid name"},
		{name: "unsupported backend", res: plainResources{}, dbName: "db", want: "database unavailable"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := &CoreAssets{res: tt.res}
			db, err := a.OpenDatabase(tt.dbName)
			if err == nil || err.Error() != tt.want {
				t.Fatalf("OpenDatabase(%q) = (%v, %v), want error %q", tt.dbName, db, err, tt.want)
			}
		})
	}
}
