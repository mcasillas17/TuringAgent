package integrations

import (
	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
)

// provider describes one third-party service: what credential it takes, and
// what holding that credential actually allows.
//
// The grants are the honest part. A pasted credential's scope is decided
// wherever it was minted, so this catalogue does not offer scope checkboxes it
// could not enforce — it states, in the user's words, what the credential
// they are about to paste can do. Nothing here is pre-ticked because nothing
// here is optional.
type provider struct {
	kind turingv1.IntegrationProvider
	// storageKey is the stable database identity. Protobuf enum values are
	// also stable: changing support must never renumber the wire contract.
	storageKey        string
	displayName       string
	category          string
	supported         bool
	unsupportedReason string
	secretLabel       string
	secretHelp        string
	accountLabel      string
	requiresEndpoint  bool
	endpointLabel     string
	grants            []string
}

// catalogue is ordered the way it should be shown: what can be connected
// first, then what cannot, so the list does not open on a wall of refusals.
var catalogue = []provider{
	{
		kind:         turingv1.IntegrationProvider_INTEGRATION_PROVIDER_GITHUB,
		storageKey:   "github",
		displayName:  "GitHub",
		category:     "Code",
		supported:    true,
		secretLabel:  "Personal access token",
		secretHelp:   "Create a fine-grained token at github.com/settings/tokens and scope it to the repositories you want reachable.",
		accountLabel: "GitHub username",
		grants: []string{
			"Exactly what the token allows — GitHub decided that when you created it, and this app cannot narrow it afterwards.",
			"A classic token with repo scope reaches every repository your account can reach, private ones included.",
			"A fine-grained token reaches only the repositories and permissions you picked, which is why it is the one to prefer.",
		},
	},
	{
		kind:              turingv1.IntegrationProvider_INTEGRATION_PROVIDER_IMAP,
		storageKey:        "imap",
		displayName:       "Mail (IMAP)",
		category:          "Mail",
		supported:         false,
		unsupportedReason: "IMAP tools are not implemented. New connections are unavailable. Accounts saved by earlier releases remain available for explicit revoke or removal.",
	},
	{
		kind:              turingv1.IntegrationProvider_INTEGRATION_PROVIDER_CALDAV,
		storageKey:        "caldav",
		displayName:       "Calendar (CalDAV)",
		category:          "Calendar",
		supported:         false,
		unsupportedReason: "CalDAV tools are not implemented. New connections are unavailable. Accounts saved by earlier releases remain available for explicit revoke or removal.",
	},
	{
		kind:              turingv1.IntegrationProvider_INTEGRATION_PROVIDER_NOTION,
		storageKey:        "notion",
		displayName:       "Notion",
		category:          "Notes",
		supported:         false,
		unsupportedReason: "Notion tools are not implemented. New connections are unavailable. Accounts saved by earlier releases remain available for explicit revoke or removal.",
	},
	{
		kind:              turingv1.IntegrationProvider_INTEGRATION_PROVIDER_GOOGLE_WORKSPACE,
		storageKey:        "google_workspace",
		displayName:       "Google (Gmail, Calendar, Drive)",
		category:          "Mail",
		supported:         false,
		unsupportedReason: "Google account connections and tools are not implemented in TuringAgent. IMAP tools are also unavailable.",
	},
	{
		kind:              turingv1.IntegrationProvider_INTEGRATION_PROVIDER_MICROSOFT_365,
		storageKey:        "microsoft_365",
		displayName:       "Microsoft 365 / Outlook",
		category:          "Mail",
		supported:         false,
		unsupportedReason: "Microsoft 365 / Outlook account connections and tools are not implemented in TuringAgent.",
	},
	{
		kind:              turingv1.IntegrationProvider_INTEGRATION_PROVIDER_SLACK,
		storageKey:        "slack",
		displayName:       "Slack",
		category:          "Chat",
		supported:         false,
		unsupportedReason: "Slack account connections and tools are not implemented in TuringAgent.",
	},
}

func lookupProvider(kind turingv1.IntegrationProvider) (provider, bool) {
	for _, candidate := range catalogue {
		if candidate.kind == kind {
			return candidate, true
		}
	}
	return provider{}, false
}

// providerByStorageKey maps a stored row back to its descriptor. A row written
// by a newer build with a provider this one has never heard of is reported as
// unknown rather than guessed at.
func providerByStorageKey(key string) (provider, bool) {
	for _, candidate := range catalogue {
		if candidate.storageKey == key {
			return candidate, true
		}
	}
	return provider{}, false
}

func (p provider) descriptor() *turingv1.ProviderDescriptor {
	return &turingv1.ProviderDescriptor{
		Provider:          p.kind,
		DisplayName:       p.displayName,
		Category:          p.category,
		Supported:         p.supported,
		UnsupportedReason: p.unsupportedReason,
		SecretLabel:       p.secretLabel,
		SecretHelp:        p.secretHelp,
		AccountLabel:      p.accountLabel,
		RequiresEndpoint:  p.requiresEndpoint,
		EndpointLabel:     p.endpointLabel,
		Grants:            append([]string(nil), p.grants...),
	}
}
