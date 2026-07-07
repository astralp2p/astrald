package tcp

import (
	"github.com/cryptopunkscc/astral-go/api/ip"
	"github.com/cryptopunkscc/astral-go/api/tcp"
)

func (mod *Module) PublicIPCandidates() (list []ip.IP) {
	for _, e := range mod.endpoints() {
		te, ok := e.Endpoint.(*tcp.Endpoint)
		if !ok {
			continue
		}

		if te.IP.IsPublic() {
			list = append(list, te.IP)
		}
	}

	return
}
