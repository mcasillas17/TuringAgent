package mcpregistry

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mcasillas17/TuringAgent/turing-backend/mcpwire"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
)

func TestLocalContainerTransportIsBoundedAndRejectsRedirects(t *testing.T) {
	service := New(nil, nil, nil)
	transport := service.clientFor(repository.MCPServerRecord{
		Tier: repository.MCPServerTierLocalContainer,
	})
	if transport.Timeout <= 0 {
		t.Fatal("local-container client has no whole-request timeout")
	}
	// Redirect refusal is asserted on the client that actually makes the
	// request. newMCPClient applies it to whatever transport it is handed, so
	// the invariant holds for this tier no matter what clientFor returns —
	// which is stronger than asserting it on the transport alone.
	client := newMCPClient("http://mcp.internal", "token", transport)
	if client.httpClient.CheckRedirect == nil {
		t.Fatal("local-container client accepts redirects")
	}
	if err := refusedRedirect(client.httpClient); err == nil {
		t.Fatal("local-container client's redirect policy permits a redirect")
	}
	if client.httpClient.Timeout != transport.Timeout {
		t.Fatalf("newMCPClient dropped the tier's timeout: %v", client.httpClient.Timeout)
	}
	if transport.CheckRedirect != nil {
		t.Fatal("newMCPClient mutated the caller's own transport")
	}
}

func TestARegisteredPeerCannotRedirectTheSealedBearerElsewhere(t *testing.T) {
	// The property test above proves the policy is installed. This proves it
	// actually stops a redirect, on the path that talks to genuinely untrusted
	// remote endpoints: this POST body travels under a registered server's
	// sealed vendor bearer, and a redirect target is chosen by the peer.
	var elsewhereSawAuthorization, elsewhereSawBody string
	elsewhere := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		elsewhereSawAuthorization = r.Header.Get("authorization")
		body, _ := io.ReadAll(r.Body)
		elsewhereSawBody = string(body)
		w.Header().Set("content-type", "application/json")
		// A literal id is safe here and only here: this target must never be
		// reached, and the test fails if it is. A fixture a client actually reads
		// has to echo the id the request carried.
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
	}))
	defer elsewhere.Close()

	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method string          `json:"method"`
			ID     json.RawMessage `json:"id"`
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &req)
		if req.Method == mcpwire.MethodInitialize {
			w.Header().Set("content-type", "application/json")
			// Echo the id: a client numbers from a random point, so answering a
			// literal fails the handshake and this test never reaches the
			// redirect it exists to refuse.
			fmt.Fprintf(w,
				`{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":%q,"capabilities":{"tools":{}},"serverInfo":{"name":"vendor","version":"1"}}}`,
				req.ID, mcpwire.ProtocolVersion)
			return
		}
		if req.Method == "" {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		http.Redirect(w, r, elsewhere.URL, http.StatusTemporaryRedirect)
	}))
	defer peer.Close()

	_, err := newMCPClient(peer.URL, "sealed-vendor-bearer", peer.Client()).
		callTool(context.Background(), "vendor.write", map[string]any{"payload": "confidential"})
	if err == nil {
		t.Fatal("callTool followed a peer-chosen redirect; it must refuse")
	}
	if elsewhereSawAuthorization != "" {
		t.Errorf("the sealed bearer was replayed to the redirect target: %q", elsewhereSawAuthorization)
	}
	if elsewhereSawBody != "" {
		t.Errorf("the request body was replayed to the redirect target: %s", elsewhereSawBody)
	}
}

// refusedRedirect exercises a client's redirect policy the way net/http does,
// with a real request and history, and returns whatever it refuses with.
func refusedRedirect(client *http.Client) error {
	if client.CheckRedirect == nil {
		return nil
	}
	from, _ := http.NewRequest(http.MethodPost, "http://mcp.internal/mcp", nil)
	to, _ := http.NewRequest(http.MethodPost, "http://elsewhere.invalid/mcp", nil)
	return client.CheckRedirect(to, []*http.Request{from})
}
