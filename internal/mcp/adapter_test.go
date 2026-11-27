package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Juanqu261/conn/internal/httpclient"
)

// TestClientNameSelectsParticipantAdapter protects requirement 9: a client
// that identifies itself as something other than Kiro is attributed with the
// generic mcp adapter, and Kiro keeps the kiro adapter.
func TestClientNameSelectsParticipantAdapter(t *testing.T) {
	for _, tc := range []struct{ client, want string }{
		{"claude-code", adapterGeneric},
		{"Kiro CLI", adapterKiro},
	} {
		t.Run(tc.client, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			var mu sync.Mutex
			var seen []string
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				seen = append(seen, r.Header.Get("X-Conn-Participant-Adapter"))
				mu.Unlock()
				http.NotFound(w, r)
			}))
			defer backend.Close()
			backendClient, err := httpclient.New(backend.URL, backend.Client())
			if err != nil {
				t.Fatal(err)
			}
			cfg := Config{Client: func() (*httpclient.Client, error) { return backendClient, nil }, DefaultProject: "019a8f00-0001-7000-8000-000000000001", adapter: &adapterIdentity{}}
			server := sdk.NewServer(&sdk.Implementation{Name: "conn-adapter-test", Version: "0"}, nil)
			detectAdapter(server, cfg.adapter)
			interactions := registerTools(server, cfg)
			defer interactions.clear()
			serverTransport, clientTransport := sdk.NewInMemoryTransports()
			serverSession, err := server.Connect(ctx, serverTransport, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer serverSession.Close()
			session, err := sdk.NewClient(&sdk.Implementation{Name: tc.client, Version: "0"}, nil).Connect(ctx, clientTransport, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close()
			_, _ = session.CallTool(ctx, &sdk.CallToolParams{Name: "read_radar", Arguments: map[string]any{"project_id": "019a8f00-0001-7000-8000-000000000001"}})
			mu.Lock()
			defer mu.Unlock()
			if len(seen) == 0 {
				t.Fatal("the tool made no backend request")
			}
			for _, adapter := range seen {
				if adapter != tc.want {
					t.Fatalf("adapter header = %q, want %q", adapter, tc.want)
				}
			}
		})
	}
}
