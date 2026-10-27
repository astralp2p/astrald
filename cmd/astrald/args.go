package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/astralp2p/astrald/lib/paths"
)

const nodeDirName = "astrald"

type Args struct {
	Root    string
	Ghost   bool
	Version bool
}

func parseArgs() (*Args, error) {
	var root string
	var ghost, version bool

	flag.StringVar(&root, "root", "", "set node's root directory (config goes to <root>/config, data to <root>/data)")
	flag.BoolVar(&ghost, "g", false, "enable ghost mode")
	flag.BoolVar(&version, "v", false, "show version")
	flag.Parse()

	args := &Args{
		Ghost:   ghost,
		Version: version,
	}

	if root != "" {
		abs, err := paths.FromFlag(root)
		if err != nil {
			return nil, fmt.Errorf("-root: %w", err)
		}
		args.Root = abs
	}

	return args, nil
}

// nodeRoots returns the config and data roots: <root>/config and <root>/data when root is set,
// the platform defaults otherwise.
func nodeRoots(root string) (configRoot, dataRoot string, err error) {
	if root != "" {
		return filepath.Join(root, "config"), filepath.Join(root, "data"), nil
	}

	if configRoot, err = defaultConfigRoot(); err != nil {
		return "", "", err
	}

	if dataRoot, err = defaultDataRoot(); err != nil {
		return "", "", err
	}

	return configRoot, dataRoot, nil
}

func userConfigDir() (string, error) {
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		return os.UserConfigDir()
	}

	return xdgDir("XDG_CONFIG_HOME", ".config")
}

func userDataDir() (string, error) {
	// On macOS and Windows, there is no XDG-style split between config
	// and data — both live under the platform's config directory.
	// Only Linux uses separate XDG_CONFIG_HOME and XDG_DATA_HOME.
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		return os.UserConfigDir()
	}

	return xdgDir("XDG_DATA_HOME", filepath.Join(".local", "share"))
}

// xdgDir returns $env when it is absolute, else fallback under $HOME.
//
// why: the XDG base directory spec makes a relative value invalid and ignored, and a relative
// $HOME would put the node under the working directory.
func xdgDir(env, fallback string) (string, error) {
	if dir, ok := paths.FromEnv(os.Getenv(env)); ok {
		return dir, nil
	}

	home, ok := paths.FromEnv(os.Getenv("HOME"))
	if !ok {
		return "", fmt.Errorf("neither $%v nor $HOME is an absolute path", env)
	}

	return filepath.Join(home, fallback), nil
}

func defaultConfigRoot() (string, error) {
	cfgDir, err := userConfigDir()
	if err != nil {
		return "", err
	}

	// why: os.UserConfigDir joins a relative $HOME on macOS
	if err = paths.CheckAbs(cfgDir); err != nil {
		return "", fmt.Errorf("config directory: %w", err)
	}

	return filepath.Join(cfgDir, nodeDirName), nil
}

func defaultDataRoot() (string, error) {
	dataDir, err := userDataDir()
	if err != nil {
		return "", err
	}

	if err = paths.CheckAbs(dataDir); err != nil {
		return "", fmt.Errorf("data directory: %w", err)
	}

	return filepath.Join(dataDir, nodeDirName), nil
}
