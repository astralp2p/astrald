package ckcc

import (
	"bufio"
	"bytes"
	"fmt"
	"os/exec"
	"strings"

	"github.com/astralp2p/astrald/mod/coldcard"
)

type Device struct {
	Serial string
}

func NewDevice(serial string) *Device {
	return &Device{Serial: serial}
}

// List enumerates connected Coldcards by parsing the output of the external ckcc CLI.
func List() (devices []*Device, err error) {
	stdout, err := run("list")
	if err != nil {
		return nil, err
	}

	scanner := bufio.NewScanner(bytes.NewReader(stdout))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if serial, ok := strings.CutPrefix(line, "Coldcard "); ok {
			serial = strings.TrimSuffix(serial, ":")

			devices = append(devices, NewDevice(serial))
		}
	}

	return
}

// PubKey returns the public key at the given derivation path; an empty path defaults to coldcard.BIP44Path.
func (c *Device) PubKey(path string) (string, error) {
	if len(path) == 0 {
		path = coldcard.BIP44Path
	}

	stdout, err := run("-s", c.Serial, "pubkey", path)
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(stdout)), nil
}

// Msg signs msg with the key at the given derivation path; an empty path defaults to coldcard.BIP44Path.
func (c *Device) Msg(msg string, path string) (string, error) {
	if len(path) == 0 {
		path = coldcard.BIP44Path
	}

	stdout, err := run("-s", c.Serial, "msg", "-p", path, "-j", msg)
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(stdout)), nil
}

// run invokes the ckcc CLI and returns its standard output.
func run(args ...string) (stdout []byte, err error) {
	cmd := exec.Command("ckcc", args...)

	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr

	err = cmd.Run()
	if err != nil {
		return nil, commandError(err, strings.TrimSpace(stderr.String()))
	}

	return out.Bytes(), nil
}

// commandError names the failed ckcc invocation with the reason the CLI or the
// device printed.
//
// why: the exec error is wrapped and kept, so a missing ckcc executable answers
// errors.Is(err, exec.ErrNotFound) rather than an opaque exit status.
// note: an exit status alone never carries the reason a locked or missing device
// names, so the standard error is appended to it.
func commandError(err error, stderr string) error {
	if len(stderr) == 0 {
		return err
	}

	return fmt.Errorf("%w: %v", err, stderr)
}
