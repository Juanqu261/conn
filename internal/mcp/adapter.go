package mcp

import (
	"context"
	"strings"
	"sync"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Participant adapter kinds. Attribution changes and authority does not.
const (
	adapterKiro    = "kiro"
	adapterGeneric = "mcp"
)

// adapterIdentity attributes this MCP process's Participant from the client
// name the MCP client reports. One stdio process serves one client.
type adapterIdentity struct {
	mu   sync.RWMutex
	name string
}

// kind returns kiro for Kiro and for a client that reported no name, and mcp
// for any other MCP client. A nil identity is Kiro.
func (a *adapterIdentity) kind() string {
	if a == nil {
		return adapterKiro
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	return adapterKind(a.name)
}

func (a *adapterIdentity) clientName() string {
	if a == nil {
		return ""
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.name
}

func (a *adapterIdentity) observe(name string) {
	if name == "" {
		return
	}
	a.mu.Lock()
	a.name = name
	a.mu.Unlock()
}

// adapterKind maps a reported client name to a participant adapter kind.
// Kiro CLI and the Kiro IDE report names containing "kiro"; Kiro CLI grew
// from the Amazon Q Developer CLI, whose names are also Kiro.
func adapterKind(name string) string {
	lower := strings.ToLower(strings.TrimSpace(name))
	switch {
	case lower == "":
		return adapterKiro
	case strings.Contains(lower, "kiro"), strings.HasPrefix(lower, "q-cli"), strings.HasPrefix(lower, "q cli"),
		strings.HasPrefix(lower, "q_cli"), strings.HasPrefix(lower, "q dev"), strings.HasPrefix(lower, "q-dev"),
		strings.HasPrefix(lower, "amazon q"), strings.HasPrefix(lower, "amazon-q"):
		return adapterKiro
	}
	return adapterGeneric
}

// detectAdapter records the client name from every request, so the
// attribution follows clientInfo from initialization or request metadata.
func detectAdapter(server *sdk.Server, identity *adapterIdentity) {
	server.AddReceivingMiddleware(func(next sdk.MethodHandler) sdk.MethodHandler {
		return func(ctx context.Context, method string, req sdk.Request) (sdk.Result, error) {
			if params, ok := req.GetParams().(*sdk.InitializeParams); ok && params != nil && params.ClientInfo != nil {
				identity.observe(params.ClientInfo.Name)
			} else if reporter, ok := req.(interface{ ClientInfo() *sdk.Implementation }); ok {
				if info := reporter.ClientInfo(); info != nil {
					identity.observe(info.Name)
				}
			}
			return next(ctx, method, req)
		}
	})
}
