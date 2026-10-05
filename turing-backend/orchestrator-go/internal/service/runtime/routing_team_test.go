package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
)

func teamCapabilities(version int32, tools ...string) *turingv1.WorkerCapabilities {
	capabilities := modelCapabilities(turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA, "llama3.2", 8192, 1)
	capabilities.TeamProtocolVersion = version
	for _, tool := range tools {
		capabilities.Tools = append(capabilities.Tools, &turingv1.DiscoveredTool{
			ServerName: "system", ToolName: tool, Schema: &structpb.Struct{},
		})
	}
	return capabilities
}

func teamRoute(minimum int) repository.RoutingRequirements {
	return repository.RoutingRequirements{
		AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2",
		MinimumTeamProtocolVersion: minimum,
	}
}

func TestDecodedWorkerCapabilitiesKeepTheTeamProtocolVersion(t *testing.T) {
	decoded, _, err := decodeWorkerCapabilities(teamCapabilities(1))
	if err != nil {
		t.Fatal(err)
	}
	if decoded.teamProtocolVersion != 1 || cloneRegisteredWorkerCapabilities(decoded).teamProtocolVersion != 1 {
		t.Fatalf("decoded version = %d, want 1 after decode and clone", decoded.teamProtocolVersion)
	}
	if got := repositoryRoutingCapabilities(decoded).TeamProtocolVersion; got != 1 {
		t.Fatalf("claim capabilities carry version %d, want 1", got)
	}
}

// The post-claim re-check compares the worker's version with the job's
// minimum, so a capability change between claim and send cannot hand a team
// job to an older worker.
func TestWorkerSupportsATeamRouteOnlyAtTheMinimumVersion(t *testing.T) {
	older, _, err := decodeWorkerCapabilities(teamCapabilities(0))
	if err != nil {
		t.Fatal(err)
	}
	current, _, err := decodeWorkerCapabilities(teamCapabilities(1))
	if err != nil {
		t.Fatal(err)
	}
	if workerCapabilitiesSupportRoute(older, teamRoute(1)) {
		t.Fatal("a version-0 worker was offered a team job")
	}
	if !workerCapabilitiesSupportRoute(current, teamRoute(1)) {
		t.Fatal("a version-1 worker was refused a team job")
	}
	if !workerCapabilitiesSupportRoute(older, teamRoute(0)) {
		t.Fatal("the team minimum became a version floor on ordinary work")
	}
	job := routingRequirementsForJob(repository.Job{
		AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2", MinimumTeamProtocolVersion: 1,
	})
	if job.MinimumTeamProtocolVersion != 1 {
		t.Fatalf("claimed job's route minimum = %d, want 1", job.MinimumTeamProtocolVersion)
	}
}

func TestValidateRoutingReportsAMissingTeamProtocolWorker(t *testing.T) {
	h := newHarness(t)
	older := connectWorkerCapabilities(t, h, "worker-team-v0", "registration-team-v0", teamCapabilities(0))
	defer func() { _ = older.CloseSend() }()

	if err := h.service.ValidateRouting(context.Background(), teamRoute(0)); err != nil {
		t.Fatalf("ordinary route refused: %v", err)
	}
	err := h.service.ValidateRouting(context.Background(), teamRoute(1))
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("team route with only a version-0 worker: %v, want FailedPrecondition", err)
	}
	detail := routingUnavailableDetail(t, err)
	if detail.GetKind() != turingv1.RoutingRequirementKind_ROUTING_REQUIREMENT_KIND_PROVIDER || detail.GetRequested() != "team protocol v1" {
		t.Fatalf("detail = %v, want provider %q", detail, "team protocol v1")
	}

	current := connectWorkerCapabilities(t, h, "worker-team-v1", "registration-team-v1", teamCapabilities(1))
	defer func() { _ = current.CloseSend() }()
	if err := h.service.ValidateRouting(context.Background(), teamRoute(1)); err != nil {
		t.Fatalf("team route with a version-1 worker: %v", err)
	}
}

// EgressToolNames intersects every worker compatible with the route. With a
// team minimum, an older worker serving the same model must not narrow it.
func TestEgressToolNamesWithATeamMinimumLeavesOlderWorkersOut(t *testing.T) {
	h := newHarness(t)
	older := connectWorkerCapabilities(t, h, "worker-tools-v0", "registration-tools-v0", teamCapabilities(0, "common"))
	defer func() { _ = older.CloseSend() }()
	current := connectWorkerCapabilities(t, h, "worker-tools-v1", "registration-tools-v1", teamCapabilities(1, "common", "newer"))
	defer func() { _ = current.CloseSend() }()

	if got := h.service.EgressToolNames(teamRoute(0)); !slices.Equal(got, []string{"system/common"}) {
		t.Fatalf("without a minimum = %v, want the intersection of both workers", got)
	}
	if got := h.service.EgressToolNames(teamRoute(1)); !slices.Equal(got, []string{"system/common", "system/newer"}) {
		t.Fatalf("with minimum 1 = %v, want the version-1 worker's tools", got)
	}
}

func TestRoutingFingerprintChangesWithTheTeamProtocolMinimum(t *testing.T) {
	if routingRequirementsFingerprint(teamRoute(0)) == routingRequirementsFingerprint(teamRoute(1)) {
		t.Fatal("a route gaining a team minimum kept its fingerprint, so its wait notice would not refresh")
	}
}

// AgentJob carries no team minimum, so the orchestrator keeps the claimed
// job's minimum on the assignment and re-checks it at the final delivery
// fence: a worker that drops to version 0 between claim and send never
// receives a gated job, and the job goes back to the queue without charging
// an execution attempt. The same assignment on a version-1 worker is sent.
func TestDeliveryFenceKeepsTheTeamMinimumOfAClaimedJob(t *testing.T) {
	for _, test := range []struct {
		version   int32
		delivered bool
	}{{version: 0, delivered: false}, {version: 1, delivered: true}} {
		t.Run(fmt.Sprintf("version %d", test.version), func(t *testing.T) {
			h := newHarness(t)
			ctx := context.Background()
			enqueued := h.enqueueRun(t, "gated delivery")
			if _, err := h.database.ExecContext(ctx, `
				UPDATE jobs SET payload_json = json_set(payload_json, '$.minimumTeamProtocolVersion', 1) WHERE id = ?`,
				enqueued.JobID); err != nil {
				t.Fatal(err)
			}
			job, err := h.repo.ClaimNextCompatibleJobWithLimit(ctx, "general_assistant", "worker-team-fence", 0, 0, nil, nil)
			if err != nil || job.RunID != enqueued.RunID {
				t.Fatalf("claim = %+v, %v", job, err)
			}
			capabilities, _, err := decodeWorkerCapabilities(teamCapabilities(test.version))
			if err != nil {
				t.Fatal(err)
			}
			claimed := assignmentForClaim(job)
			connected := &worker{
				commands:       make(chan workerCommand, 1),
				done:           make(chan struct{}),
				registrationID: "registration-team-fence",
				capabilities:   capabilities,
				maxConcurrent:  1,
				lastHeartbeat:  time.Now().UTC(),
				assignments:    map[string]assignment{job.RunID: claimed},
			}
			h.service.mu.Lock()
			h.service.workers["worker-team-fence"] = connected
			h.service.mu.Unlock()
			stream := &reconnectAcceptanceStream{ctx: ctx, assigned: make(chan struct{})}
			command := &turingv1.RuntimeCommand{Command: &turingv1.RuntimeCommand_RunAssigned{RunAssigned: claimed.job}}
			if err := h.service.sendCommand(ctx, stream, workerCommand{command: command}, connected, "worker-team-fence"); err != nil {
				t.Fatal(err)
			}
			delivered := false
			select {
			case <-stream.assigned:
				delivered = true
			default:
			}
			if delivered != test.delivered {
				t.Fatalf("delivered = %v, want %v", delivered, test.delivered)
			}
			if test.delivered {
				return
			}
			run, err := h.repo.GetRun(ctx, enqueued.RunID)
			if err != nil || run.Status != "queued" || run.ExecutionActive {
				t.Fatalf("run after the fence = %+v, %v; want queued and inactive", run, err)
			}
			var attempt int
			if err := h.database.QueryRowContext(ctx, `SELECT attempt FROM jobs WHERE id = ?`, enqueued.JobID).Scan(&attempt); err != nil {
				t.Fatal(err)
			}
			if attempt != 1 {
				t.Fatalf("job attempt = %d, want 1: a capability fence is not an execution failure", attempt)
			}
		})
	}
}

// The TUR-010 path end to end: with only a version-0 worker live, a gated job
// stays queued and its wait notice names the team protocol, while the same
// worker still takes ordinary work from another session. A version-1 worker
// then takes the gated job.
func TestATeamGatedJobWaitsWithANoticeWhileOrdinaryWorkRuns(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	older := registerWorkerCapabilities(t, h, "worker-gate-v0", "registration-gate-v0", teamCapabilities(0))
	gated := h.enqueueRun(t, "gated")
	if _, err := h.database.ExecContext(ctx, `
		UPDATE jobs SET payload_json = json_set(payload_json, '$.minimumTeamProtocolVersion', 1) WHERE id = ?`,
		gated.JobID); err != nil {
		t.Fatal(err)
	}
	plain := h.enqueueRun(t, "plain")

	if err := h.service.refreshPendingCapabilityState(ctx, "test", "", true, false); err != nil {
		t.Fatal(err)
	}
	type notice struct{ runID, payloadJSON string }
	var notices []notice
	rows, err := h.database.QueryContext(ctx, `
		SELECT run_id, payload_json FROM events
		WHERE type = 'agent.run.step' AND json_extract(payload_json, '$.reason') = 'routing_capability_unavailable'`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var n notice
		if err := rows.Scan(&n.runID, &n.payloadJSON); err != nil {
			_ = rows.Close()
			t.Fatal(err)
		}
		notices = append(notices, n)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		t.Fatal(err)
	}
	if len(notices) != 1 || notices[0].runID != gated.RunID {
		t.Fatalf("wait notices = %v, want exactly one for the gated run %s", notices, gated.RunID)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(notices[0].payloadJSON), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["unavailableCapability"] != turingv1.RoutingRequirementKind_ROUTING_REQUIREMENT_KIND_PROVIDER.String() ||
		payload["requested"] != "team protocol v1" || payload["minimumTeamProtocolVersion"] != float64(1) {
		t.Fatalf("notice = %v, want provider %q with minimum 1", payload, "team protocol v1")
	}

	if err := h.service.DispatchPending(ctx); err != nil {
		t.Fatal(err)
	}
	if got := assignedRun(t, older); got != plain.RunID {
		t.Fatalf("version-0 worker took run %s, want the ordinary run %s", got, plain.RunID)
	}
	var status string
	if err := h.database.QueryRowContext(ctx, `SELECT status FROM jobs WHERE id = ?`, gated.JobID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "pending" {
		t.Fatalf("gated job status = %q, want pending", status)
	}

	current := registerWorkerCapabilities(t, h, "worker-gate-v1", "registration-gate-v1", teamCapabilities(1))
	if err := h.service.DispatchPending(ctx); err != nil {
		t.Fatal(err)
	}
	if got := assignedRun(t, current); got != gated.RunID {
		t.Fatalf("version-1 worker took run %s, want the gated run %s", got, gated.RunID)
	}
}

func assignedRun(t *testing.T, connected *worker) string {
	t.Helper()
	select {
	case command := <-connected.commands:
		return command.command.GetRunAssigned().GetRunId()
	default:
		t.Fatal("no run was assigned to the worker")
		return ""
	}
}
