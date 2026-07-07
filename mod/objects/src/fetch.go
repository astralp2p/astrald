package objects

import (
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/lib/query"
	"github.com/cryptopunkscc/astrald/lib/arl"
	"github.com/cryptopunkscc/astrald/mod/objects"
)

func (mod *Module) fetch(addr string) (objectID *astral.ObjectID, err error) {
	switch {
	case isURL(addr):
		return mod.fetchURL(addr)

	case isARL(addr):
		var a *arl.ARL

		a, err = arl.Parse(addr, mod.Dir)
		if err != nil {
			return
		}
		return mod.fetchARL(a)
	}

	return objectID, errors.New("scheme not supported")
}

func (mod *Module) fetchURL(url string) (objectID *astral.ObjectID, err error) {
	// Make a GET request to the URL
	response, err := http.Get(url)
	if err != nil {
		return
	}
	defer response.Body.Close()

	var alloc = max(response.ContentLength, 0)

	ctx := astral.NewContext(nil).WithIdentity(mod.node.Identity())

	w, err := mod.WriteDefault().Create(ctx, &objects.CreateOpts{
		Alloc: int(alloc),
	})
	if err != nil {
		return
	}
	defer w.Discard()

	_, err = io.Copy(w, response.Body)
	if err != nil {
		return
	}

	return w.Commit()
}

func (mod *Module) fetchARL(a *arl.ARL) (objectID *astral.ObjectID, err error) {
	if a.Caller.IsZero() {
		a.Caller = mod.node.Identity()
	}

	var q = astral.NewQuery(a.Caller, a.Target, a.Query)

	conn, err := query.RouteInFlight(mod.ctx, mod.node, astral.Launch(q))
	if err != nil {
		return
	}

	ctx := astral.NewContext(nil).WithIdentity(mod.node.Identity())

	w, err := mod.WriteDefault().Create(ctx, nil)
	if err != nil {
		return
	}
	defer w.Discard()

	io.Copy(w, conn)

	return w.Commit()
}

func isURL(url string) bool {
	matched, _ := regexp.Match("^https?://", []byte(url))
	return matched
}

func isARL(s string) bool {
	return strings.HasPrefix(s, "astral://")
}
