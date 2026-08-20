package runtime

import (
	"context"
	"log"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	approvalsvc "github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/approvals"
)

// filesServerName is the only MCP server whose tools write into the sandbox and
// therefore the only one that receives a provenance capability. Sending one to
// a server that does not expect it would be rejected as an unknown _meta key.
const filesServerName = "files"

// provenanceIssuer is satisfied by the real approval service, which owns the
// signing secret. It is an optional interface for the same reason
// unattendedApprover is: an orchestrator wired without it is a possible state,
// and the failure it produces is a file tool call mcp-files refuses rather than
// a compile error here.
type provenanceIssuer interface {
	IssueToolProvenance(ctx context.Context, req approvalsvc.ProvenanceRequest) (string, error)
}

// issueToolProvenance mints the capability that accompanies one file tool call.
//
// Everything it binds is already server-side state: the run's session, the
// deletion generation the approval service reads for itself, and the canonical
// args hash the orchestrator computed from the beacon it recorded. The worker
// contributes nothing but the call it is asking about.
//
// A failure is logged and returns an empty token rather than failing the
// decision, because the call still cannot succeed: mcp-files requires a
// capability, so an unissued one is refused at the point of the write with a
// message about provenance rather than turning every policy decision into an
// approval-service outage.
func (s *Server) issueToolProvenance(ctx context.Context, beacon *turingv1.ToolCallBeacon, run repository.Run, argsHash string) string {
	if beaconServerName(beacon) != filesServerName {
		return ""
	}
	issuer, ok := s.approvals.(provenanceIssuer)
	if !ok {
		log.Printf("issue provenance for %s: approval service cannot issue provenance capabilities", beacon.GetToolCallId())
		return ""
	}
	token, err := issuer.IssueToolProvenance(ctx, approvalsvc.ProvenanceRequest{
		SessionID:   run.SessionID,
		RunID:       beacon.GetRunId(),
		AgentID:     "general_assistant",
		ToolName:    beacon.GetToolName(),
		ArgsHash:    argsHash,
		LogicalPath: beaconLogicalPath(beacon),
	})
	if err != nil {
		log.Printf("issue provenance for %s: %v", beacon.GetToolCallId(), err)
		return ""
	}
	return token
}

// beaconLogicalPath reads the path argument the capability is scoped to. A tool
// without one (files.list defaults to the sandbox root) is scoped to the root,
// which is what an omitted path already means.
func beaconLogicalPath(beacon *turingv1.ToolCallBeacon) string {
	args := beaconArgs(beacon)
	path, _ := args["path"].(string)
	return path
}

// withToolProvenance re-issues a capability for a decision replayed from an
// already-recorded tool call. A retried beacon gets the same answer it got the
// first time, and it needs the same capability with it — without one the retry
// would be told it may proceed and then be refused at the write.
//
// Denials get nothing: there is no call to authorise.
func (s *Server) withToolProvenance(ctx context.Context, decision *turingv1.ToolPolicyDecision, beacon *turingv1.ToolCallBeacon, run repository.Run, argsHash string) *turingv1.ToolPolicyDecision {
	if decision == nil || decision.GetDecision() == turingv1.ToolPolicyDecision_DECISION_DENY {
		return decision
	}
	decision.ProvenanceToken = s.issueToolProvenance(ctx, beacon, run, argsHash)
	return decision
}
