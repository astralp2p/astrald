package all

// This file includes all modules that should be compiled into the node.
// Do not move this file to another package as it is exposed for https://github.com/cryptopunkscc/portal.

import (
	_ "github.com/cryptopunkscc/astrald/mod/apphost/src"
	_ "github.com/cryptopunkscc/astrald/mod/archives/src"
	_ "github.com/cryptopunkscc/astrald/mod/auth/src"
	_ "github.com/cryptopunkscc/astrald/mod/bip137sig/src"
	_ "github.com/cryptopunkscc/astrald/mod/coldcard/src"
	_ "github.com/cryptopunkscc/astrald/mod/crypto/src"
	_ "github.com/cryptopunkscc/astrald/mod/dir/src"
	_ "github.com/cryptopunkscc/astrald/mod/ether/src"
	_ "github.com/cryptopunkscc/astrald/mod/events/src"
	_ "github.com/cryptopunkscc/astrald/mod/exonet/src"
	_ "github.com/cryptopunkscc/astrald/mod/fs/src"
	_ "github.com/cryptopunkscc/astrald/mod/fwd/src"
	_ "github.com/cryptopunkscc/astrald/mod/gateway/src"
	_ "github.com/cryptopunkscc/astrald/mod/indexing/src"
	_ "github.com/cryptopunkscc/astrald/mod/ip/src"
	//_ "github.com/cryptopunkscc/astrald/mod/kcp/src"
	_ "github.com/cryptopunkscc/astrald/mod/kcp/src"
	_ "github.com/cryptopunkscc/astrald/mod/log/src"
	_ "github.com/cryptopunkscc/astrald/mod/nat/src"
	_ "github.com/cryptopunkscc/astrald/mod/nearby/src"
	_ "github.com/cryptopunkscc/astrald/mod/nodes/src"
	_ "github.com/cryptopunkscc/astrald/mod/objects/src"
	_ "github.com/cryptopunkscc/astrald/mod/scheduler/src"
	_ "github.com/cryptopunkscc/astrald/mod/secp256k1/src"
	_ "github.com/cryptopunkscc/astrald/mod/services/src"
	_ "github.com/cryptopunkscc/astrald/mod/shell/src"
	_ "github.com/cryptopunkscc/astrald/mod/tcp/src"
	_ "github.com/cryptopunkscc/astrald/mod/tor/src"
	_ "github.com/cryptopunkscc/astrald/mod/tree/src"
	_ "github.com/cryptopunkscc/astrald/mod/user/src"
)
