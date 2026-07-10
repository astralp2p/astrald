package ckcc

import (
	"bufio"
	"bytes"
	"errors"
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
	cmd := exec.Command("ckcc", "list")

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	switch {
	case err == nil:
	default:
		return nil, err
	}

	scanner := bufio.NewScanner(&stdout)
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

	cmd := exec.Command("ckcc", "-s", c.Serial, "pubkey", path)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(stdout.String()), nil
}

// Msg signs msg with the key at the given derivation path; an empty path defaults to coldcard.BIP44Path.
func (c *Device) Msg(msg string, path string) (string, error) {
	if len(path) == 0 {
		path = coldcard.BIP44Path
	}

	cmd := exec.Command("ckcc", "-s", c.Serial, "msg", "-p", path, "-j", msg)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		return "", errors.New(strings.TrimSpace(stderr.String()))
	}

	return strings.TrimSpace(stdout.String()), nil
}
