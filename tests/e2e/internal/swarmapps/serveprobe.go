package main

import (
	apphostcli "github.com/astralp2p/astral-go/api/apphost/client"
	"github.com/astralp2p/astral-go/api/services"
	servicescli "github.com/astralp2p/astral-go/api/services/client"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/astrald"
)

// ProbeService is the service serveProbe tries to advertise.
const ProbeService = "serve-probe"

// serveProbe tries, as the app c's token names, the two acts ServeApps
// guards: apphost.register_handler, at an endpoint nothing listens on, and a
// private services.advertise of ProbeService. It reports each as "ok" or the
// error the node answered, and undoes what succeeded.
//
// why: an app whose ContractRequest is pending or declined holds no
// ServeApps, so the node refuses both (apphost op_register_handler.go,
// services op_advertise.go); before G7 a revoked grant showed the same.
func serveProbe(ctx *astral.Context, c *astrald.Client) (map[string]any, error) {
	out := map[string]any{"register_handler": "ok", "advertise": "ok"}
	if err := apphostcli.New(nil, c).RegisterHandler(ctx, "tcp:127.0.0.1:1", astral.NewNonce()); err != nil {
		out["register_handler"] = err.Error()
	}
	b, err := servicescli.New(nil, c).AdvertiseOpen(ctx, []string{ProbeService}, false,
		func(*astral.Context, *astral.Identity, string) (*services.Update, error) {
			return &services.Update{Available: true}, nil
		})
	if err != nil {
		out["advertise"] = err.Error()
	} else {
		_ = b.Close()
	}
	return out, nil
}
