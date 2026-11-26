package contracts

import (
	"os"
	"path/filepath"

	"github.com/astralp2p/astral-go/astral"
)

// Waiting tells the harness answerer (tests/lib/answerer.py) that a driver
// waits for an answer, until the returned func runs: a file in
// $ASTRAL_TESTS_WAITING. Outside the harness it does nothing.
//
// why: the answerer queries the nodes only while a driver waits, so a test
// that judges a quiet node log finds no answerer traffic in it.
func Waiting() (done func()) {
	dir := os.Getenv("ASTRAL_TESTS_WAITING")
	if dir == "" {
		return func() {}
	}
	path := filepath.Join(dir, "go-"+astral.NewNonce().String())
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		return func() {}
	}
	return func() { _ = os.Remove(path) }
}
