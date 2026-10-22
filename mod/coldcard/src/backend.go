package coldcard

import "github.com/astralp2p/astrald/mod/coldcard/ckcc"

// DeviceBackend is the module's access to the attached ColdCards.
//
// why: a host may reach its devices another way, and a test holds no device, so
// the access is one value the module holds rather than a call into a package.
// why one interface for three calls: they address the same set of devices, and
// the module holds one implementation of all three.
type DeviceBackend interface {
	// List returns the serials of the connected devices.
	List() ([]string, error)

	// ReadPubKey returns the hex public key of the device with serial, derived at
	// path.
	ReadPubKey(serial string, path string) (string, error)

	// SignMessage signs msg with the key of the device with serial, derived at
	// path, and returns the signature as base64.
	SignMessage(serial string, msg string, path string) (string, error)
}

// ckccBackend reaches the devices through the ckcc CLI.
//
// fixme: the only implementation shells out to a POSIX CLI, so a Windows host has
// no backend to inject.
type ckccBackend struct{}

func (ckccBackend) List() (serials []string, err error) {
	devices, err := ckcc.List()
	if err != nil {
		return nil, err
	}

	for _, dev := range devices {
		serials = append(serials, dev.Serial)
	}

	return serials, nil
}

func (ckccBackend) ReadPubKey(serial string, path string) (string, error) {
	return ckcc.NewDevice(serial).PubKey(path)
}

func (ckccBackend) SignMessage(serial string, msg string, path string) (string, error) {
	return ckcc.NewDevice(serial).Msg(msg, path)
}
