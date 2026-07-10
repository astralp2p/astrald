package core

import (
	"errors"
	"fmt"
	"time"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astrald/core/assets"
	"github.com/astralp2p/astrald/resources"
)

const logTag = "node"

var _ astral.Node = &Node{}

// Node is the core astral node: identity, config, assets, modules, and the embedded query Router.
type Node struct {
	*Router
	identity *astral.Identity
	config   Config

	assets  *assets.CoreAssets
	modules *Modules

	startedAt time.Time
	log       *log.Logger
}

// NewNode instantiates a new node
func NewNode(nodeID *astral.Identity, res resources.Resources) (*Node, error) {
	var err error

	if res == nil {
		res = resources.NewMemResources()
	}

	var node = &Node{
		identity: nodeID,
		config:   defaultConfig,
	}

	// router
	node.Router = NewRouter(node)

	// initialize basic logger
	node.initLogger()

	node.assets, err = assets.NewCoreAssets(res, nil)
	if err != nil {
		return nil, fmt.Errorf("database error: %w", err)
	}

	// node config
	err = node.assets.LoadYAML(configName, &node.config)
	if err != nil {
		if !errors.Is(err, resources.ErrNotFound) {
			return nil, fmt.Errorf("error loading config: %w", err)
		}
	}

	// modules
	var enabled = node.config.Modules
	if enabled == nil {
		enabled = modules.Keys()
	}
	node.modules, err = NewModules(node, enabled, node.assets, node.log)
	if err != nil {
		return nil, fmt.Errorf("error creating module manager: %w", err)
	}

	return node, nil
}

func (node *Node) Modules() *Modules {
	return node.modules
}

// Identity returns node's identity
func (node *Node) Identity() *astral.Identity {
	return node.identity
}

func (node *Node) StartedAt() time.Time {
	return node.startedAt
}
