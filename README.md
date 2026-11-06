astrald
=======

## Overview

**The Astral Network** is an abstract, general-purpose peer-to-peer network layer that runs on top of multiple transports. **astrald** is a daemon that provides easy access to the network via local interface. It provides connectivity, identity, authentication, encryption and a set of core protocols apps and agents can use to easily interact over the network. The full documentation is [here](https://github.com/astralp2p/astral-docs/blob/master/README.md).

## Network architecture

All actors (nodes, apps, users and agents) on the network have a self-assigned identity based on a ECDSA key on the secp256k1 curve, the same as the Lightning Network or Bitcoin. Nodes form the core infrastructure of the network by establish encrypted and authenticated links with each other, exchanging certificates, gossip data and organizing complex p2p processes. Apps, agents and users access the network via the API exposed by the daemon via varoius transports - astral apphost protocol, HTTP, WSS, etc.

Actors send each other queries (similar to HTTP query path) over the network that can be rejected or accepted to establish a connection. The connection is then used to exchange data in form of Objects (or just a raw bytestream).

## Quick start

Install the daemon and the tooling:

```shellsession
go install \
  github.com/astralp2p/astrald/cmd/astrald@latest \
  github.com/astralp2p/astrald/cmd/astral-query@latest
```

Run the daemon:

```shellsession
$ astrald
```

The node will start and generate a new identity and an alias for itself. Once the node has started verify it is accepting local API calls. `user.info` answers without a token and reports the node's state, `fresh` for a new node; `.spec` lists the node's operations:

```shellsession
$ astral-query user.info -out json
{"Type":"mod.user.info","Object":{"NodeAlias":"","UserAlias":"","ContractID":null,"Contract":null,"State":"fresh","Manager":null}}
$ astral-query localnode:.spec -out json
```

## Setting up the node

A new node belongs to nobody. Setup has two steps: one local app becomes the node's manager, and the manager claims the node for a user. A distribution's setup app does both. On a server, or for an AI agent, the shell does them with `astral-query`. [Node Setup](https://github.com/astralp2p/astral-docs/blob/master/topics/node-setup.md) explains the states, the manager and the claim; [Setting up from a shell](https://github.com/astralp2p/astral-docs/blob/master/topics/node-setup.md#setting-up-from-a-shell) has every command.

Until a manager is appointed, any local program or browser page can appoint itself. To close that window, write `apphost.yaml` before the first start, as [Before the first start](https://github.com/astralp2p/astral-docs/blob/master/topics/node-setup.md#before-the-first-start) shows: the node gets a token, and only a session holding it appoints the manager.

### Appoint the manager

The request takes on no policy, so app registrations, swarm joins and swarm invitations keep refusing. Without a token, or with the node's token when `apphost.yaml` sets one:

```shellsession
$ echo '{"Type":"mod.apphost.register_manager_request","Object":{"Name":"cli-setup","GrantPermits":[],"ContractPermits":[],"Policies":[]}}' \
    | astral-query apphost.register_manager -in json -out json
{"Type":"apphost.access_token","Object":{"Identity":"03864ef0...","Token":"k7m2q5x9r3v4n8p1","ExpiresAt":"2036-05-25T12:00:00+02:00"}}
$ export ASTRALD_APPHOST_TOKEN=k7m2q5x9r3v4n8p1
```

### Claim for a new user

The node generates 24 words and returns the user they derive, and claims nothing. Write the words down: they are the user's only backup. The same words then claim the node, and the reply carries the user's token.

```shellsession
$ astral-query user.derive_identity -generate true -out json
{"Type":"mod.user.derive_identity_result","Object":{"Identity":"02e96bd9...","Mnemonic":"penalty stadium garlic ... rifle fitness garage"}}
$ echo '{"Type":"string16","Object":"penalty stadium garlic ... rifle fitness garage"}' | astral-query user.bootstrap -in json -out json
{"Type":"mod.user.bootstrap_result","Object":{"Identity":"02e96bd9...","Mnemonic":"","Token":"b9c2e1a3d4f5867a"}}
```

### Claim for an existing user

The node claims itself for the user that the given BIP-39 words derive. `user.derive_identity` with the same words previews that user first. To bring the node into a swarm the user already runs, see [User and Swarm](https://github.com/astralp2p/astral-docs/blob/master/topics/user-and-swarm.md).

```shellsession
$ echo '{"Type":"string16","Object":"horse soldier ... term change"}' | astral-query user.bootstrap -in json -out json
{"Type":"mod.user.bootstrap_result","Object":{"Identity":"02564ba7...","Mnemonic":"","Token":"b9c2e1a3d4f5867a"}}
```

Check the claim; `user.info` reports `"State":"claimed"`:

```shellsession
$ astral-query user.info -out json
```

## Access for apps and AI agents

App registration refuses by default, so the user creates an agent's identity and token with `mcp.create_agent`, under the user's token:

```shellsession
$ export ASTRALD_APPHOST_TOKEN=b9c2e1a3d4f5867a
$ astral-query mcp.create_agent -alias agent -out json
{"Type":"mcp.agent","Object":{"Identity":"02f40933...","Alias":"agent","Token":"h4d8s2w6y1b9t3n7","ExpiresAt":"2027-11-06T00:49:56Z"}}
$ ASTRALD_APPHOST_TOKEN=h4d8s2w6y1b9t3n7 astral-query apphost.whoami -out json
{"Type":"identity","Object":"02f40933..."}
```

Each further action is a grant the user writes with [`apphost.grant`](https://github.com/astralp2p/astral-docs/blob/master/protocols/apphost/ops/apphost.grant.md). The agent never needs the user's token, the manager's token or the words. Apps register with [`apphost.register`](https://github.com/astralp2p/astral-docs/blob/master/protocols/apphost/ops/apphost.register.md) once the manager or the user sets the app register policy ([Node Policies](https://github.com/astralp2p/astral-docs/blob/master/topics/node-policies.md)).

Tokens work over any of the supported transports (native or [HTTP](https://github.com/astralp2p/astral-docs/blob/master/topics/http-transport.md)).

## Hardware identity

Astral can use BIP-0137-compatible hardware wallets as the source for user identities. An app holds the device and signs for its key through [`crypto.register_signer`](https://github.com/astralp2p/astral-docs/blob/master/protocols/crypto/ops/crypto.register_signer.md). See [astral-coldcard](https://git.satforge.dev/astralp2p/astral-coldcard).