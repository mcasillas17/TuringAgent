// Package telemetry serves what this installation has been doing, aggregated
// from the orchestrator's own database.
//
// Read-only and local-only. There is one RPC, it takes a window and returns
// counts, and there is no write side at all: every number it reports was
// recorded by some other subsystem doing its actual work, so nothing here can
// produce a figure the rest of the system did not.
package telemetry

import (
	"context"
	"errors"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// DefaultWindowDays is what a request that names no window gets. A week is the
// span in which a personal machine's usage is still recognisable as particular
// days rather than as a trend.
const DefaultWindowDays = 7

type Server struct {
	turingv1.UnimplementedTelemetryServiceServer
	repo *repository.Repository
	// now is a seam. Every window is measured back from one instant, and a
	// test that cannot pin that instant cannot assert anything about a
	// "last 7 days" figure without depending on when it runs.
	now func() time.Time
}

func New(repo *repository.Repository) *Server {
	return &Server{repo: repo, now: func() time.Time { return time.Now().UTC() }}
}

// NewWithClock builds a server whose windows are measured from a fixed
// instant. Tests only.
func NewWithClock(repo *repository.Repository, now func() time.Time) *Server {
	return &Server{repo: repo, now: now}
}

func (s *Server) GetTelemetrySummary(ctx context.Context, req *turingv1.GetTelemetrySummaryRequest) (*turingv1.GetTelemetrySummaryResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	days := int(req.GetWindowDays())
	if days == 0 {
		days = DefaultWindowDays
	}
	summary, err := s.repo.TelemetrySummary(ctx, s.now(), days)
	if err != nil {
		// Out of range is the caller's mistake and is named as such, so a
		// client can correct it. Nothing else is: a storage failure collapses
		// to Internal with a fixed message rather than handing its text out.
		if errors.Is(err, repository.ErrTelemetryWindowOutOfRange) {
			return nil, status.Errorf(codes.InvalidArgument,
				"window must be between %d and %d days",
				repository.MinTelemetryWindowDays, repository.MaxTelemetryWindowDays)
		}
		return nil, status.Error(codes.Internal, "telemetry summary failed")
	}
	return toProto(summary), nil
}

func toProto(summary repository.TelemetrySummary) *turingv1.GetTelemetrySummaryResponse {
	return &turingv1.GetTelemetrySummaryResponse{
		Window: &turingv1.TelemetryWindow{
			Days:  int32(summary.Window.Days),
			Start: timestamppb.New(summary.Window.Start),
			End:   timestamppb.New(summary.Window.End),
		},
		Runs: &turingv1.RunTotals{
			Total:             summary.Runs.Total,
			Completed:         summary.Runs.Completed,
			Failed:            summary.Runs.Failed,
			Cancelled:         summary.Runs.Cancelled,
			InFlight:          summary.Runs.InFlight,
			AverageDurationMs: summary.Runs.AverageDurationMs,
		},
		Tokens: &turingv1.TokenTotals{
			InputTokens:      summary.Tokens.InputTokens,
			OutputTokens:     summary.Tokens.OutputTokens,
			RunsWithUsage:    summary.Tokens.RunsWithUsage,
			RunsWithoutUsage: summary.Tokens.RunsWithoutUsage,
		},
		Tools:          toolsToProto(summary.Tools),
		Models:         modelsToProto(summary.Models),
		ExternalAgents: externalAgentsToProto(summary.ExternalAgents),
		Automations: &turingv1.AutomationTotals{
			Runs:                summary.Automations.Runs,
			Completed:           summary.Automations.Completed,
			Failed:              summary.Automations.Failed,
			UnattendedApprovals: summary.Automations.UnattendedApprovals,
		},
		Integrations: &turingv1.IntegrationTotals{
			Connected: summary.Integrations.Connected,
			Revoked:   summary.Integrations.Revoked,
		},
		Daily: dailyToProto(summary.Daily),
	}
}

func toolsToProto(tools []repository.TelemetryToolUsage) []*turingv1.ToolUsage {
	converted := make([]*turingv1.ToolUsage, 0, len(tools))
	for _, tool := range tools {
		converted = append(converted, &turingv1.ToolUsage{
			ServerName:        tool.ServerName,
			ToolName:          tool.ToolName,
			Calls:             tool.Calls,
			Failed:            tool.Failed,
			Denied:            tool.Denied,
			AverageDurationMs: tool.AverageDurationMs,
		})
	}
	return converted
}

func modelsToProto(models []repository.TelemetryModelUsage) []*turingv1.ModelUsage {
	converted := make([]*turingv1.ModelUsage, 0, len(models))
	for _, model := range models {
		converted = append(converted, &turingv1.ModelUsage{
			Provider:         model.Provider,
			Model:            model.Model,
			Runs:             model.Runs,
			InputTokens:      model.InputTokens,
			OutputTokens:     model.OutputTokens,
			RunsWithoutUsage: model.RunsWithoutUsage,
		})
	}
	return converted
}

func externalAgentsToProto(agents []repository.TelemetryExternalAgentUsage) []*turingv1.ExternalAgentUsage {
	converted := make([]*turingv1.ExternalAgentUsage, 0, len(agents))
	for _, agent := range agents {
		converted = append(converted, &turingv1.ExternalAgentUsage{
			DisplayName:      agent.DisplayName,
			EndpointHost:     agent.EndpointHost,
			Runs:             agent.Runs,
			InputTokens:      agent.InputTokens,
			OutputTokens:     agent.OutputTokens,
			RunsWithoutUsage: agent.RunsWithoutUsage,
		})
	}
	return converted
}

func dailyToProto(daily []repository.TelemetryDailyActivity) []*turingv1.DailyActivity {
	converted := make([]*turingv1.DailyActivity, 0, len(daily))
	for _, day := range daily {
		converted = append(converted, &turingv1.DailyActivity{
			Date:         day.Date,
			Runs:         day.Runs,
			ToolCalls:    day.ToolCalls,
			InputTokens:  day.InputTokens,
			OutputTokens: day.OutputTokens,
		})
	}
	return converted
}
