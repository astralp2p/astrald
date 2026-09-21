package tc

import (
	"encoding/hex"
	"errors"
	"io/ioutil"
)

// Authenticate selects an auth method supported by both the daemon and this client and performs it.
// Returns an error if the daemon's protocol info is unavailable, or if no mutually supported
// method is available.
func (ctl *Control) Authenticate() error {
	info := ctl.ProtocolInfo()
	if info == nil {
		return errors.New("protocol info unavailable")
	}

	if info.HasAuthMethod(authMethodCookie) {
		return ctl.authenticateWithCookie(info)
	}

	return errors.New("no supported auth method")
}

func (ctl *Control) authenticateWithCookie(info *ProtocolInfo) error {
	bytes, err := ioutil.ReadFile(info.AuthCookieFile)
	if err != nil {
		return err
	}

	cookie := hex.EncodeToString(bytes)

	_, _, err = ctl.request("AUTHENTICATE %s", cookie)

	return err
}
