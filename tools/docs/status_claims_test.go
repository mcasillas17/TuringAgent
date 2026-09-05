package docs

// These witnesses describe the bounded capability named by each table row.
// A status alone proves nothing: every witness must still hold, including the
// positive contracts that justify a pending limitation. See README.md here for
// assurance levels and the reconciliation procedure when implementations move.
func featureStatusClaims() []statusClaim {
	const app = "turing-client/turing_app/"
	const backend = "turing-backend/"
	const orchestrator = backend + "orchestrator-go/internal/"
	const runtime = backend + "agent-runtime-go/internal/"
	const shell = app + "lib/ui/shell/responsive_shell.dart"
	const api = app + "lib/networking/grpc_client.dart"
	const integrations = orchestrator + "service/integrations/"
	const registry = orchestrator + "service/mcpregistry/"
	const roadmap = "docs/NORTH_STAR.md"
	has := func(file, snippet string) statusEvidence {
		return statusEvidence{path: file, require: snippet}
	}
	function := func(file, name, snippet string) statusEvidence {
		return statusEvidence{path: file, symbol: name, require: snippet}
	}
	pendingTask := func(id string) statusEvidence {
		return statusEvidence{path: roadmap, pattern: `(?m)^#{3,4}\s+(?:[0-9]+\.\s+)?` + id + `\s+-\s+\S`, limitation: "remaining capability tracked by " + id}
	}
	remote := []statusEvidence{
		function(runtime+"agent/external_agent.go", "NewExternalAgentProviderFunc", "llm.NewOpenAICompatibleWithLimits("),
		function(runtime+"agent/general_assistant.go", "providerFor", "a.externalAgents(target)"),
		has("proto/turing/v1/agents.proto", "rpc SetSessionAgent("),
		function(runtime+"agent/external_agent_test.go", "TestExternalAgentProviderResolvesTheNamedKey", `provider.ID() != "openai_compatible"`),
		has(backend+"agent-runtime-go/cmd/runtime/main.go", "executor.SetExternalAgentProvider(agent.NewExternalAgentProviderFunc("),
		has(app+"lib/features/workspace/agents_page.dart", "widget.apiClient.createExternalAgent("),
		has(app+"lib/features/workspace/agents_page.dart", "widget.apiClient.updateExternalAgent("),
		has(api, "_externalAgents.createExternalAgent("),
		has(api, "_externalAgents.updateExternalAgent("),
		has(orchestrator+"repository/jobs.go", `"externalAgent": resolvedRoute.externalTarget`),
		function(orchestrator+"service/chat/egress.go", "applyRemoteEgress", "req.GetRemoteEgressConsent()"),
		function(orchestrator+"service/chat/egress_test.go", "TestPrepareExternalAgentDisclosureOmitsCrossSessionRecall", "h.chatClient.PrepareRemoteEgress("),
		function(orchestrator+"service/chat/egress_test.go", "TestSendMessageRejectsRemoteRunWithoutConsentBeforePersistence", "codes.FailedPrecondition"),
		has(shell, "SessionAgentBar("),
		has(orchestrator+"app/app.go", "turingv1.RegisterExternalAgentServiceServer(publicServer, agentService)"),
		function(orchestrator+"service/agents/service_test.go", "TestSessionAgentRoundTripThroughTheService", "SetSessionAgent("),
	}
	claims := []statusClaim{
		{"proto-breaking", false, []statusEvidence{
			{path: ".github/workflows/ci.yml", workflowJob: "proto-and-scripts", require: `tools/proto/breaking.sh "origin/${GITHUB_BASE_REF:-main}"`},
			{path: ".github/workflows/ci.yml", workflowJob: "proto-and-scripts", require: `TURING_REQUIRE_BUF=1 go test ./tools/proto -run '^TestBreaking' -count=1`},
			{path: "tools/proto/breaking.sh", pattern: `(?m)^buf breaking "\$ROOT/proto" --against "\$baseline/proto"$`},
			function("tools/proto/breaking_test.go", "TestBreakingCompatibility", `fixture: "removed"`),
			{path: "buf.yaml", yamlPath: "breaking/use", pattern: `^\s*-\s+FILE\s*$`},
		}},
		{"proto-codegen", false, []statusEvidence{
			{path: ".github/workflows/ci.yml", workflowJob: "proto-and-scripts", require: "tools/proto/check.sh"},
			{path: "tools/proto/check.sh", pattern: `(?m)^"\$ROOT/tools/proto/generate\.sh"$`},
			function("tools/docs/status_codegen_test.go", "TestProtoCheckRejectsGeneratedDrift", `protoCheckBehaviorProblem(t, script, changed)`),
		}},
		{"flutter-search", true, []statusEvidence{
			has(shell, "builder: (_) => SearchScreen(apiClient: widget.apiClient,"),
			has(shell, "onSearch: _openSearch"),
			has(app+"lib/features/search/search_screen.dart", "widget.apiClient.searchMessages(query: query, limit: 50)"),
			has(app+"lib/networking/api_client.dart", "Future<List<SearchHit>> searchMessages("),
			has(api, "await _sessions.searchMessages("),
			has("proto/turing/v1/sessions.proto", "rpc SearchMessages("),
			has(orchestrator+"app/app.go", "turingv1.RegisterSessionServiceServer(publicServer, sessionService)"),
			function(orchestrator+"service/sessions/service.go", "SearchMessages", "s.search.SearchMessages("),
			function(orchestrator+"service/sessions/service_test.go", "TestSessionServiceSearchMessagesReturnsGlobalAndScopedResults", "client.SearchMessages("),
			has(app+"test/features/search/search_screen_test.dart", "testWidgets('sends a limit of 50 to searchMessages',"),
			has(app+"test/networking/grpc_client_test.dart", "test('searchMessages prefers hits and ignores duplicate messages',"),
			has(app+"lib/features/search/search_screen.dart", "widget.onOpenSession("),
			has(shell, "onOpenSession: (sessionId) async"),
			has(app+"test/features/search/search_screen_test.dart", "testWidgets('tapping a result opens the exact session ID',"),
		}},
		{"flutter-workspace", true, []statusEvidence{
			has(shell, "case ShellDestination.chats: return _conversation(palette);"),
			has(shell, "child: ChatScreen("),
			{path: app + "lib/ui/shell/shell_destination.dart", require: "implemented: true", absent: "implemented: false"},
			has(app+"test/ui/shell_navigation_test.dart", "testWidgets('every destination opens something real',"),
			has(shell, "widget.apiClient.listSessionPage("),
			has(app+"lib/features/chat/chat_screen.dart", "widget.apiClient.listMessages("),
			has(app+"test/ui/responsive_shell_backend_test.dart", "testWidgets('the shell is one surface: conversations beside a chat',"),
		}},
		{"mcp-registry", true, []statusEvidence{
			has(registry+"service.go", "s.repo.RegisterMCPServer(ctx, repository.ImportedMCPServer{"),
			has("proto/turing/v1/mcp.proto", "rpc RegisterMcpServer("),
			has(orchestrator+"app/app.go", "turingv1.RegisterMcpRegistryServiceServer(publicServer, mcpregistrysvc.NewPublicServer(mcpRegistryService))"),
			has(app+"lib/features/workspace/workspace_pages.dart", "await widget.apiClient.registerMcpServer("),
			has(api, "await _mcpRegistry.registerMcpServer("),
			function(registry+"register_test.go", "TestRegisterMcpServerArrivesDisabledWithDerivedTierAndSealedToken", "RegisterMcpServer("),
		}},
		{"mcp-lifecycle", true, []statusEvidence{
			function(runtime+"mcp/client.go", "ListTools", `c.request(ctx, "tools/list", params)`),
			function(runtime+"mcp/client.go", "CallTool", `c.request(ctx, "tools/call", params)`),
			function(runtime+"mcp/client_test.go", "TestListToolsPaginatesInOrder", "client.ListTools("),
			pendingTask("CON-001"),
		}},
		{"remote-model-routing", true, remote},
		{"agent-delegation", true, []statusEvidence{
			{path: runtime + "agent/external_agent.go", symbol: "NewExternalAgentProviderFunc", require: "llm.NewOpenAICompatibleWithLimits(", limitation: "the external-agent adapter performs model inference"},
			{path: runtime + "agent/general_assistant.go", rejectPattern: `(?i)a2a|agent[_-]?to[_-]?agent`},
			{path: orchestrator + "app/app.go", rejectPattern: `(?i)a2a|agent[_-]?to[_-]?agent`},
			pendingTask("A2A-001"),
		}},
		{"github-tools", true, []statusEvidence{
			function(integrations+"tools.go", "ListIntegrationTools", `connection.Provider == "github"`),
			function(integrations+"call.go", "CallIntegrationTool", "s.callGitHubGuarded("),
			has("proto/turing/v1/integrations.proto", "rpc CallIntegrationTool("),
			has(orchestrator+"app/app.go", "turingv1.RegisterIntegrationServiceServer(internalServer, integrationsvc.NewInternalServer(integrationService))"),
			function(integrations+"consumer_test.go", "TestGitHubCredentialTravelsOnlyInAuthorizationHeader", "server.callGitHub("),
			function(integrations+"consumer_test.go", "TestZeroConnectionsZeroToolsThenConnectRevokeReconnect", "server.ListIntegrationTools("),
			function(integrations+"call.go", "CallIntegrationTool", "s.approvals.ConsumeApprovalForThirdParty("),
			function(integrations+"call.go", "CallIntegrationTool", "s.validateIntegrationDecision("),
			function(integrations+"call.go", "validateIntegrationDecision", "s.repo.RunAllowsIntegration("),
			function(integrations+"consumer_test.go", "TestIntegrationDispatchRequiresAllFourDecisionLegsBeforeNetwork", "server.CallIntegrationTool("),
			function(integrations+"consumer_test.go", "TestIntegrationWriteRequiresOneArgumentBoundApprovalIncludingConnectionID", "CallIntegrationTool("),
			has(registry+"service.go", `if req.GetServerName() == "integrations" && policy == "safe" && !toolpolicy.ToolReadOnly(req.GetServerName(), req.GetToolName())`),
			function(registry+"pseudo_policy_test.go", "TestUpdateToolPolicyByNameGuardsRoundTripsAndNotifies", "UpdateToolPolicyByName("),
		}},
		{"other-integration-tools", true, []statusEvidence{
			{path: integrations + "tools.go", symbol: "lookupIntegrationTool", require: "range githubTools", rejectPattern: `(?s)\brange\b.*\brange\b`, limitation: "the consumer lookup only exposes GitHub tools"},
			{path: integrations + "tools.go", rejectPattern: `name:\s*"(?:imap|caldav|notion)\.`},
			function(integrations+"call.go", "CallIntegrationTool", `sealed.Provider != "github"`),
			function(integrations+"service.go", "ConnectAccount", "s.repo.CreateConnection("),
			pendingTask("INT-001"),
		}},
		{"mobile-client", true, []statusEvidence{
			{path: app + "android/app/src/main/AndroidManifest.xml", absent: "android.permission.INTERNET", limitation: "the main Android manifest does not grant network access"},
			has(app+"android/app/src/debug/AndroidManifest.xml", "android.permission.INTERNET"),
			has(app+"android/app/src/profile/AndroidManifest.xml", "android.permission.INTERNET"),
			has(shell, "constraints.maxWidth < compactBreakpoint"),
			pendingTask("MOB-001"),
		}},
		{"mobile-reachability", true, []statusEvidence{
			{path: backend + "infra/docker-compose.yml", yamlPath: "services/turing-orchestrator/ports", pattern: `^\s*-\s+127\.0\.0\.1:\$\{ORCHESTRATOR_PUBLIC_PORT:-3000\}:\$\{ORCHESTRATOR_PUBLIC_PORT:-3000\}\s*$`, limitation: "Compose publishes only a loopback port"},
			has(orchestrator+"app/app.go", "auth.UnaryInterceptor(cfg.ClientAPIKey, publicAuth)"),
			function(orchestrator+"auth/interceptor.go", "UnaryInterceptor", "TokenMatches(token, requiredToken)"),
			has(backend+"orchestrator-go/cmd/server/main.go", `net.Listen("tcp", fmt.Sprintf(":%d", cfg.PublicPort))`),
			pendingTask("SEC-001"),
		}},
	}
	addEvidence := func(id string, evidence ...statusEvidence) {
		for i := range claims {
			if claims[i].id == id {
				claims[i].evidence = append(claims[i].evidence, evidence...)
				return
			}
		}
		panic("unknown status claim: " + id)
	}
	for _, method := range []struct{ dart, goName string }{
		{"getSessionAgent", "GetSessionAgent"},
		{"setSessionAgent", "SetSessionAgent"},
		{"clearSessionAgent", "ClearSessionAgent"},
	} {
		addEvidence("remote-model-routing",
			has(app+"lib/features/workspace/session_agent_bar.dart", "widget.apiClient."+method.dart+"("),
			has(api, "_externalAgents."+method.dart+"("),
			function(orchestrator+"service/agents/service.go", method.goName, "s.repo."+method.goName+"("),
		)
	}
	// Read-side wiring for every current workspace surface, not the enum's
	// implemented flag. Page tests exercise these calls through fake APIs; the
	// normal Flutter/Go suites remain the behavioral gates.
	for _, page := range []struct{ destination, class, file, method, client, service string }{
		{"skills", "SkillsPage", "skills_page.dart", "listSkills", "_skills", "Skill"},
		{"memory", "MemoryPage", "memory_page.dart", "listMemoryState", "_memory", "Memory"},
		{"integrations", "IntegrationsPage", "integrations_page.dart", "listConnections", "_integrations", "Integration"},
		{"mcps", "McpsPage", "workspace_pages.dart", "listMcpServers", "_mcpRegistry", "McpRegistry"},
		{"automations", "AutomationsPage", "automations_page.dart", "listAutomations", "_automations", "Automation"},
		{"agents", "AgentsPage", "agents_page.dart", "listExternalAgents", "_externalAgents", "ExternalAgent"},
		{"telemetry", "TelemetryPage", "telemetry_page.dart", "getTelemetrySummary", "_telemetry", "Telemetry"},
	} {
		addEvidence("flutter-workspace",
			has(shell, "case ShellDestination."+page.destination+": return "+page.class+"(apiClient: widget.apiClient"),
			has(app+"lib/features/workspace/"+page.file, "widget.apiClient."+page.method+"("),
			has(api, page.client+"."+page.method+"("),
			has(orchestrator+"app/app.go", "turingv1.Register"+page.service+"ServiceServer(publicServer,"),
			has(app+"test/ui/"+page.destination+"_test.dart", "testWidgets("),
			has(app+"test/ui/"+page.destination+"_test.dart", page.class+"("),
		)
	}
	for _, operation := range []struct{ method, rpc, implementation, testFile, testName string }{
		{"reimportMcpJson", "ReimportMcpJson", "s.ReimportConfiguredJSON(", "reimport_rpc_test.go", "TestReimportMcpJsonRPCMapsReportFieldsAndSortsRefused"},
		{"setMcpServerEnabled", "SetMcpServerEnabled", "s.repo.SetMCPServerEnabled(", "blank_url_enable_test.go", "TestSetMcpServerEnabledStillWorksForBundledServer"},
		{"rotateMcpServerToken", "RotateMcpServerToken", "s.repo.ReplaceMCPServerToken(", "rotate_test.go", "TestRotateMcpServerTokenSealsWithServerNameAsAAD"},
		{"updateMcpToolPolicy", "UpdateMcpToolPolicy", "s.repo.SetMCPToolPolicy(", "policy_notify_order_test.go", "TestUpdateMcpToolPolicyNotifiesRegistryChangeOnSuccess"},
	} {
		addEvidence("mcp-registry",
			has(app+"lib/features/workspace/workspace_pages.dart", "widget.apiClient."+operation.method+"("),
			has(api, "_mcpRegistry."+operation.method+"("),
			has("proto/turing/v1/mcp.proto", "rpc "+operation.rpc+"("),
			has(registry+"service.go", operation.implementation),
			function(registry+operation.testFile, operation.testName, operation.rpc+"("),
		)
	}
	for _, file := range []string{runtime + "mcp/client.go", registry + "client.go", backend + "mcp-files/cmd/server/main.go", backend + "mcp-system/cmd/server/main.go"} {
		addEvidence("mcp-lifecycle",
			statusEvidence{path: file, require: `"tools/list"`, absent: `"initialize"`, limitation: "the tools transport has no initialization method"},
			statusEvidence{path: file, require: `"tools/call"`, absent: `"notifications/initialized"`},
		)
	}
	for _, tool := range []string{"github.list_issues", "github.get_issue", "github.get_file", "github.create_comment"} {
		addEvidence("github-tools",
			has(integrations+"tools.go", `name: "`+tool+`"`),
			function(integrations+"github.go", "githubRequest", `case "`+tool+`":`),
		)
	}
	for _, provider := range []string{"IMAP", "CALDAV", "NOTION"} {
		addEvidence("other-integration-tools", statusEvidence{
			path:    integrations + "providers.go",
			pattern: `(?s)kind:\s+turingv1\.IntegrationProvider_INTEGRATION_PROVIDER_` + provider + `,\s+storageKey:[^}]+?supported:\s+true,`,
		})
	}
	for _, provider := range []string{"GOOGLE_WORKSPACE", "MICROSOFT_365", "SLACK"} {
		addEvidence("other-integration-tools", statusEvidence{
			path:    integrations + "providers.go",
			pattern: `(?s)kind:\s+turingv1\.IntegrationProvider_INTEGRATION_PROVIDER_` + provider + `,\s+storageKey:[^}]+?supported:\s+false,`,
		})
	}
	return claims
}
