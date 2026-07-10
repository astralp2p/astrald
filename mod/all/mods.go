package all

// This file includes all modules that should be compiled into the node.
// Do not move this file to another package as it is exposed for https://github.com/cryptopunkscc/portal.

import (
	_ "github.com/astralp2p/astrald/mod/apphost/src"
	_ "github.com/astralp2p/astrald/mod/archives/src"
	_ "github.com/astralp2p/astrald/mod/auth/src"
	_ "github.com/astralp2p/astrald/mod/bip137sig/src"
	_ "github.com/astralp2p/astrald/mod/coldcard/src"
	_ "github.com/astralp2p/astrald/mod/crypto/src"
	_ "github.com/astralp2p/astrald/mod/dir/src"
	_ "github.com/astralp2p/astrald/mod/ether/src"
	_ "github.com/astralp2p/astrald/mod/events/src"
	_ "github.com/astralp2p/astrald/mod/exonet/src"
	_ "github.com/astralp2p/astrald/mod/fs/src"
	_ "github.com/astralp2p/astrald/mod/fwd/src"
	_ "github.com/astralp2p/astrald/mod/gateway/src"
	_ "github.com/astralp2p/astrald/mod/indexing/src"
	_ "github.com/astralp2p/astrald/mod/ip/src"
	//_ "github.com/astralp2p/astrald/mod/kcp/src"
	_ "github.com/astralp2p/astrald/mod/kcp/src"
	_ "github.com/astralp2p/astrald/mod/log/src"
	_ "github.com/astralp2p/astrald/mod/nat/src"
	_ "github.com/astralp2p/astrald/mod/nearby/src"
	_ "github.com/astralp2p/astrald/mod/nodes/src"
	_ "github.com/astralp2p/astrald/mod/objects/src"
	_ "github.com/astralp2p/astrald/mod/scheduler/src"
	_ "github.com/astralp2p/astrald/mod/secp256k1/src"
	_ "github.com/astralp2p/astrald/mod/services/src"
	_ "github.com/astralp2p/astrald/mod/shell/src"
	_ "github.com/astralp2p/astrald/mod/tcp/src"
	_ "github.com/astralp2p/astrald/mod/tor/src"
	_ "github.com/astralp2p/astrald/mod/tree/src"
	_ "github.com/astralp2p/astrald/mod/user/src"
)
