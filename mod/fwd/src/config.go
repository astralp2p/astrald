package fwd

type Config struct {
	Forwards map[string]string `yaml:"forwards"`
}

// defaultConfig returns the config a load starts from.
//
// why a function: a copied Config shares its Forwards map, and a second load
// in the same process would start from the first load's forwards.
func defaultConfig() Config {
	return Config{
		Forwards: map[string]string{},
	}
}
