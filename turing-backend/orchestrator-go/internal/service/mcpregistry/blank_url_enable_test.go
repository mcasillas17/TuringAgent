package mcpregistry

import (
	"context"
	"net/http"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// neverInvokedRoundTripper fails the test the instant RoundTrip is called:
// it stands in for "this call must never contact the network at all," a
// stronger proof than merely asserting the eventual outcome, since a
// discovery attempt against an empty URL might otherwise fail fast enough
// on its own to look indistinguishable from never having been attempted.
type neverInvokedRoundTripper struct {
	t *testing.T
}

func (r neverInvokedRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	r.t.Helper()
	r.t.Fatal("unexpected network contact: SetMcpServerEnabled must refuse a blank-url placeholder before ever reaching discovery")
	return nil, nil
}

// SetMcpServerEnabled must refuse a non-bundled server whose url is empty
// — a legacy migration-0016 placeholder awaiting a real endpoint is the
// realistic shape, but the check is on the URL itself, not on how the row
// came to have one — with FailedPrecondition, before ever calling
// repo.SetMCPServerEnabled: no repository mutation, no registry-change
// notification, no audit record, and (since a local-container or
// remote-url tier would otherwise attempt live discovery as part of
// enabling) no network contact at all.
//
// This closes the "stale migrated tools advertisement" gap: before this
// check, enabling such a placeholder would flip enabled=1 in the
// repository first and only then attempt (and fail) discovery against an
// empty URL, leaving a server that is enabled — and therefore whose
// pre-registry tool snapshot could look available to a client — despite
// never having a real endpoint at all.
func TestSetMcpServerEnabledRefusesBlankURLPlaceholderBeforeAnyMutation(t *testing.T) {
	service, repo, database := newRegistryTestServiceWithRealAudit(t)
	ctx := context.Background()
	service.httpClient = &http.Client{Transport: neverInvokedRoundTripper{t: t}}
	notifier := &countingRegistryChangeNotifier{}
	service.SetRegistryChangeNotifier(notifier)

	placeholder, err := repo.RegisterMCPServer(ctx, repository.ImportedMCPServer{
		Name: "vendor", URL: "", Tier: repository.MCPServerTierLocalContainer,
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = service.SetMcpServerEnabled(ctx, &turingv1.SetMcpServerEnabledRequest{
		ServerId: placeholder.Server.ID, Enabled: true,
	})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("code = %v, want FailedPrecondition for a blank-url placeholder", status.Code(err))
	}

	if notifier.calls != 0 {
		t.Fatalf("notify calls = %d, want 0: a refused enable must never notify", notifier.calls)
	}
	if payloads := auditPayloadsForAction(t, database, "mcp.server.enabled"); len(payloads) != 0 {
		t.Fatalf("audit payloads = %+v, want none: a refused enable must never be audited", payloads)
	}

	updated, err := repo.GetMCPServer(ctx, placeholder.Server.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Enabled {
		t.Fatal("the repository must not have been mutated by a refused enable")
	}
	if updated.Status != "unknown" || updated.StatusError != "" {
		t.Fatalf("status = %q/%q, want unchanged from registration's initial value", updated.Status, updated.StatusError)
	}
}

// The same refusal applies to a disable request against a blank-url
// placeholder too: the check is unconditional on the server's own url, not
// on which direction the request asks to flip enabled to. A placeholder is
// always already disabled by construction, so this is mostly a defensive,
// uniform rule rather than one guarding against a realistic accidental
// no-op — but it keeps the one precondition check simple and total rather
// than needing to reason about which request shapes it does and does not
// cover.
func TestSetMcpServerEnabledRefusesBlankURLPlaceholderDisableRequestToo(t *testing.T) {
	service, repo, database := newRegistryTestServiceWithRealAudit(t)
	ctx := context.Background()
	service.httpClient = &http.Client{Transport: neverInvokedRoundTripper{t: t}}
	notifier := &countingRegistryChangeNotifier{}
	service.SetRegistryChangeNotifier(notifier)

	placeholder, err := repo.RegisterMCPServer(ctx, repository.ImportedMCPServer{
		Name: "vendor", URL: "", Tier: repository.MCPServerTierLocalContainer,
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = service.SetMcpServerEnabled(ctx, &turingv1.SetMcpServerEnabledRequest{
		ServerId: placeholder.Server.ID, Enabled: false,
	})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("code = %v, want FailedPrecondition for a blank-url placeholder", status.Code(err))
	}
	if notifier.calls != 0 {
		t.Fatalf("notify calls = %d, want 0", notifier.calls)
	}
	if payloads := auditPayloadsForAction(t, database, "mcp.server.disabled"); len(payloads) != 0 {
		t.Fatalf("audit payloads = %+v, want none", payloads)
	}
}

// A remote-url-tier placeholder (not just local-container) must be refused
// the same way: the check is about the URL, not the tier.
func TestSetMcpServerEnabledRefusesBlankURLPlaceholderForRemoteURLTierToo(t *testing.T) {
	service, repo := newRegistryTestService(t)
	ctx := context.Background()
	service.httpClient = &http.Client{Transport: neverInvokedRoundTripper{t: t}}

	placeholder, err := repo.RegisterMCPServer(ctx, repository.ImportedMCPServer{
		Name: "vendor", URL: "", Tier: repository.MCPServerTierRemoteURL,
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = service.SetMcpServerEnabled(ctx, &turingv1.SetMcpServerEnabledRequest{
		ServerId: placeholder.Server.ID, Enabled: true,
	})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("code = %v, want FailedPrecondition for a blank-url placeholder", status.Code(err))
	}
}

// An ordinary, already-registered server with a real (non-empty) url must
// be entirely unaffected by this check: this is the regression guard that
// the new precondition is scoped to an empty url, not to every
// local-container enable.
func TestSetMcpServerEnabledStillWorksForNonPlaceholderServer(t *testing.T) {
	h := newRegistryCallHarness(t)
	if err := h.repo.SetMCPServerEnabled(context.Background(), h.serverID, false); err != nil {
		t.Fatal(err)
	}

	descriptor, err := h.registry.SetMcpServerEnabled(context.Background(), &turingv1.SetMcpServerEnabledRequest{
		ServerId: h.serverID, Enabled: true,
	})
	if err != nil {
		t.Fatalf("a server with a real url must still be enable-able: %v", err)
	}
	if !descriptor.GetEnabled() {
		t.Fatal("descriptor.Enabled = false, want true")
	}
}

// A bundled server always carries a real (non-empty) url (seeded by
// migration 0016), so the blank-url precondition above must never fire
// for one: enabling an already-enabled bundled server (the only shape a
// client would ever send, since bundled servers start enabled and cannot
// be disabled) must still succeed, exactly as before this precondition
// existed. This is the regression guard for that precondition being
// scoped correctly rather than accidentally over-broad.
func TestSetMcpServerEnabledStillWorksForBundledServer(t *testing.T) {
	service, repo := newRegistryTestService(t)
	ctx := context.Background()

	servers, err := repo.ListMCPServers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var bundled repository.MCPServerRecord
	found := false
	for _, server := range servers {
		if server.Tier == repository.MCPServerTierBundled {
			bundled = server
			found = true
			break
		}
	}
	if !found {
		t.Fatal("test setup is broken: no bundled server found")
	}
	if bundled.URL == "" {
		t.Fatal("test setup is broken: a bundled server must always carry a real url")
	}

	descriptor, err := service.SetMcpServerEnabled(ctx, &turingv1.SetMcpServerEnabledRequest{
		ServerId: bundled.ID, Enabled: true,
	})
	if err != nil {
		t.Fatalf("enabling an already-enabled bundled server must still succeed: %v", err)
	}
	if !descriptor.GetEnabled() {
		t.Fatal("descriptor.Enabled = false, want true")
	}
}
