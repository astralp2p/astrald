package crypto

import "github.com/astralp2p/astral-go/api/objects"

const maxObjectSize = 4096

type Config struct {
	Repos []string
}

var defaultConfig = Config{
	Repos: []string{objects.RepoLocal, objects.RepoSystem, "mem0"},
}
