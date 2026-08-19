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
	// storageKey is what lands in the database, chosen so the enum can be
	// renumbered without rewriting rows.
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
		kind:             turingv1.IntegrationProvider_INTEGRATION_PROVIDER_IMAP,
		storageKey:       "imap",
		displayName:      "Mail (IMAP)",
		category:         "Mail",
		supported:        true,
		secretLabel:      "App password",
		secretHelp:       "Create an app password in your mail provider's security settings and paste it here. Your normal account password would work too, and is exactly what you should not use: an app password can be deleted on its own.",
		accountLabel:     "Email address",
		requiresEndpoint: true,
		endpointLabel:    "IMAP server (for example imap.fastmail.com)",
		grants: []string{
			"Read every message in every mailbox on this account, including ones you have never opened.",
			"Move, flag and delete messages.",
			"An app password covers the whole mailbox. There is no read-only version of it, and this app cannot narrow it.",
		},
	},
	{
		kind:             turingv1.IntegrationProvider_INTEGRATION_PROVIDER_CALDAV,
		storageKey:       "caldav",
		displayName:      "Calendar (CalDAV)",
		category:         "Calendar",
		supported:        true,
		secretLabel:      "App password",
		secretHelp:       "iCloud, Fastmail and Nextcloud all issue app passwords for CalDAV from their account settings.",
		accountLabel:     "Account username or address",
		requiresEndpoint: true,
		endpointLabel:    "CalDAV server (for example caldav.icloud.com)",
		grants: []string{
			"Read every calendar on this account, including the events you have marked private.",
			"Create, change and delete events.",
			"The credential is per account, not per calendar: you cannot share only one calendar this way.",
		},
	},
	{
		kind:         turingv1.IntegrationProvider_INTEGRATION_PROVIDER_NOTION,
		storageKey:   "notion",
		displayName:  "Notion",
		category:     "Notes",
		supported:    true,
		secretLabel:  "Internal integration token",
		secretHelp:   "Create an internal integration at notion.so/my-integrations, then share the specific pages you want reachable with it.",
		accountLabel: "Workspace name",
		grants: []string{
			"Read every page and database you have shared with this integration.",
			"Change content in those pages, if you gave the integration write access when you created it.",
			"Nothing you have not shared with it. Notion enforces that boundary, which makes this the narrowest connection on the list.",
		},
	},
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
		kind:              turingv1.IntegrationProvider_INTEGRATION_PROVIDER_GOOGLE_WORKSPACE,
		storageKey:        "google_workspace",
		displayName:       "Google (Gmail, Calendar, Drive)",
		category:          "Mail",
		supported:         false,
		unsupportedReason: "Google's APIs only issue credentials through OAuth, which needs a client ID and secret registered to a published app plus a browser redirect back to it. TuringAgent has none of those, so a Connect button here would open a consent screen that fails at the end. If your Google account issues app passwords, Gmail can still be reached as Mail (IMAP).",
	},
	{
		kind:              turingv1.IntegrationProvider_INTEGRATION_PROVIDER_MICROSOFT_365,
		storageKey:        "microsoft_365",
		displayName:       "Microsoft 365 / Outlook",
		category:          "Mail",
		supported:         false,
		unsupportedReason: "Microsoft has retired basic authentication for Exchange Online and Outlook.com, so an app password no longer opens a mailbox there. What is left is OAuth against a registered Azure application, which TuringAgent does not have.",
	},
	{
		kind:              turingv1.IntegrationProvider_INTEGRATION_PROVIDER_SLACK,
		storageKey:        "slack",
		displayName:       "Slack",
		category:          "Chat",
		supported:         false,
		unsupportedReason: "Slack issues tokens to installed apps through an OAuth install flow with a redirect URI. There is no token a person can create by hand and paste in.",
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
