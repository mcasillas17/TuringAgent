package memory

import (
	"strings"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
)

// What MemorySettings.vault_root is for is one sentence on one card: "your
// memory lives here, open it in Obsidian". Inside Docker the vault is opened at
// `/memory`, which exists in one container and nowhere the person reading that
// card can go — so the field carries the display root, and the display root is
// the host directory Compose passes in.
//
// The field is display-only in both directions. Nothing decides anything with
// it, and nothing derives it from a path the user supplied: it is a string the
// operator configured, validated at load, handed straight through.

// The Docker case: the path shown is the one on the host, never the container's.
func TestMemorySettingsShowTheHostPathWhenTheVaultIsMountedIn(t *testing.T) {
	server, _, vault, _, ctx := newMemoryServiceStack(t, t.TempDir()+"/turing.db", t.TempDir(), nil)
	const host = "/Users/someone/turing/turing-backend/memory"
	server.SetMemoryDisplayRoot(host)

	settings, err := server.GetMemorySettings(ctx, &turingv1.GetMemorySettingsRequest{})
	if err != nil {
		t.Fatalf("GetMemorySettings: %v", err)
	}
	if settings.GetVaultRoot() != host {
		t.Fatalf("vault_root = %q, want the host path %q", settings.GetVaultRoot(), host)
	}
	// And specifically not the path only the container can see. The vault's own
	// root here is a temp directory, so this is the general claim: the field is
	// the display root and nothing else.
	if settings.GetVaultRoot() == vault.Root() {
		t.Fatal("vault_root fell back to the path the orchestrator opens")
	}
}

// The native case, and the default: nobody configured a display root, so the
// folder named is the folder opened.
func TestMemorySettingsShowTheOpenedVaultWhenThereIsNoSeparateDisplayRoot(t *testing.T) {
	server, _, vault, _, ctx := newMemoryServiceStack(t, t.TempDir()+"/turing.db", t.TempDir(), nil)
	server.SetMemoryDisplayRoot(vault.Root())

	settings, err := server.GetMemorySettings(ctx, &turingv1.GetMemorySettingsRequest{})
	if err != nil {
		t.Fatalf("GetMemorySettings: %v", err)
	}
	if settings.GetVaultRoot() != vault.Root() {
		t.Fatalf("vault_root = %q, want the vault's own root %q", settings.GetVaultRoot(), vault.Root())
	}
}

// A display root that is not a path anybody could open is omitted rather than
// rendered. Config refuses these at load, so reaching this branch means
// something else went wrong — and the honest answer to "where is my memory?"
// when nothing usable is known is to say nothing, not to name the container.
func TestMemorySettingsOmitADisplayRootThatIsNotAUsablePath(t *testing.T) {
	for _, unusable := range []string{"", "memory", "relative/memory", "../../etc", "/srv/../memory"} {
		server, _, _, _, ctx := newMemoryServiceStack(t, t.TempDir()+"/turing.db", t.TempDir(), nil)
		server.SetMemoryDisplayRoot(unusable)

		settings, err := server.GetMemorySettings(ctx, &turingv1.GetMemorySettingsRequest{})
		if err != nil {
			t.Fatalf("GetMemorySettings: %v", err)
		}
		if settings.GetVaultRoot() != "" {
			t.Fatalf("display root %q was rendered as %q, want it omitted",
				unusable, settings.GetVaultRoot())
		}
		// Writability is a fact about the vault the orchestrator opened, not
		// about the string being shown, so it stays answerable either way.
		if !settings.GetVaultWritable() {
			t.Fatal("an omitted display path also suppressed the vault's own writability")
		}
	}
}

// The container path is never presented as somewhere to go. This is the
// regression the whole setting exists for: with a host path configured, the
// mount point must not appear in what the client is told.
func TestMemorySettingsNeverPresentTheContainerMountAsTheUsersFolder(t *testing.T) {
	server, _, _, _, ctx := newMemoryServiceStack(t, t.TempDir()+"/turing.db", t.TempDir(), nil)
	server.SetMemoryDisplayRoot("/Users/someone/turing/turing-backend/memory")

	settings, err := server.GetMemorySettings(ctx, &turingv1.GetMemorySettingsRequest{})
	if err != nil {
		t.Fatalf("GetMemorySettings: %v", err)
	}
	if settings.GetVaultRoot() == "/memory" || strings.HasPrefix(settings.GetVaultRoot(), "/memory/") {
		t.Fatalf("vault_root = %q, which is a path only the container has", settings.GetVaultRoot())
	}
}

// A server with no vault has nothing to say about where memory is, whatever a
// display root was configured to. The path is a description of a vault, and
// there is no vault.
func TestMemorySettingsSayNothingAboutAFolderWithNoVaultBehindIt(t *testing.T) {
	server, _, _, _, ctx := newMemoryServiceStack(t, t.TempDir()+"/turing.db", t.TempDir(), nil)
	server.SetMemoryDisplayRoot("/Users/someone/turing/turing-backend/memory")
	vaultless := New(server.repo, nil, nil)
	vaultless.SetMemoryDisplayRoot("/Users/someone/turing/turing-backend/memory")

	settings, err := vaultless.GetMemorySettings(ctx, &turingv1.GetMemorySettingsRequest{})
	if err != nil {
		t.Fatalf("GetMemorySettings: %v", err)
	}
	if settings.GetVaultRoot() != "" {
		t.Fatalf("vault_root = %q, want nothing said about a vault that is not there", settings.GetVaultRoot())
	}
	if settings.GetUnavailableReason() != turingv1.MemoryUnavailableReason_MEMORY_UNAVAILABLE_REASON_VAULT_MISSING {
		t.Fatalf("reason = %v, want the missing vault reported", settings.GetUnavailableReason())
	}
}
