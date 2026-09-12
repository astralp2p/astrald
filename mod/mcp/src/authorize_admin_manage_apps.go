package mcp

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/routing"
)

// authorizeAdminManageApps reports whether the query's caller may administer the
// agents this node holds.
//
// mcp.create_agent, mcp.list_agents and mcp.delete_agent ask this question and
// reject the query when the answer is no, before they read or change an agent
// record and before they accept the connection.
//
// why the same action as apphost's token ops: an agent's credential is an apphost
// access token, so administering agents is administering tokens.
func (mod *Module) authorizeAdminManageApps(ctx *astral.Context, q *routing.IncomingQuery) bool {
	return mod.Auth.Authorize(ctx, &auth.AdminManageAppsAction{
		Action: auth.NewAction(q.Caller()),
	})
}
