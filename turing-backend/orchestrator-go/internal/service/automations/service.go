// Package automations serves the user's saved prompts and the schedule that
// sends them, and holds the loop that fires one when it comes due.
package automations

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

type Server struct {
	turingv1.UnimplementedAutomationServiceServer
	repo *repository.Repository
}

func New(repo *repository.Repository) *Server {
	return &Server{repo: repo}
}

func (s *Server) CreateAutomation(ctx context.Context, req *turingv1.CreateAutomationRequest) (*turingv1.Automation, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	schedule, err := scheduleFromProto(req.GetSchedule())
	if err != nil {
		return nil, err
	}
	automation, err := s.repo.CreateAutomation(ctx, repository.AutomationInput{
		Name:         req.GetName(),
		Prompt:       req.GetPrompt(),
		Schedule:     schedule,
		Enabled:      req.GetEnabled(),
		AllowedTools: toolsFromProto(req.GetAllowedTools()),
	})
	if err != nil {
		return nil, automationError(err, "create automation failed")
	}
	return toProto(automation), nil
}

func (s *Server) UpdateAutomation(ctx context.Context, req *turingv1.UpdateAutomationRequest) (*turingv1.Automation, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	schedule, err := scheduleFromProto(req.GetSchedule())
	if err != nil {
		return nil, err
	}
	automation, err := s.repo.UpdateAutomation(ctx, req.GetAutomationId(), repository.AutomationInput{
		Name:         req.GetName(),
		Prompt:       req.GetPrompt(),
		Schedule:     schedule,
		AllowedTools: toolsFromProto(req.GetAllowedTools()),
	})
	if err != nil {
		return nil, automationError(err, "update automation failed")
	}
	return toProto(automation), nil
}

func (s *Server) SetAutomationEnabled(ctx context.Context, req *turingv1.SetAutomationEnabledRequest) (*turingv1.Automation, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	automation, err := s.repo.SetAutomationEnabled(ctx, req.GetAutomationId(), req.GetEnabled())
	if err != nil {
		return nil, automationError(err, "update automation failed")
	}
	return toProto(automation), nil
}

func (s *Server) DeleteAutomation(ctx context.Context, req *turingv1.DeleteAutomationRequest) (*turingv1.DeleteAutomationResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	if err := s.repo.DeleteAutomation(ctx, req.GetAutomationId()); err != nil {
		return nil, automationError(err, "delete automation failed")
	}
	return &turingv1.DeleteAutomationResponse{}, nil
}

func (s *Server) ListAutomations(ctx context.Context, _ *turingv1.ListAutomationsRequest) (*turingv1.ListAutomationsResponse, error) {
	automations, err := s.repo.ListAutomations(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, "list automations failed")
	}
	converted := make([]*turingv1.Automation, 0, len(automations))
	for _, automation := range automations {
		converted = append(converted, toProto(automation))
	}
	return &turingv1.ListAutomationsResponse{Automations: converted}, nil
}

// automationError maps the repository's named failures to codes a client can
// act on. Everything else collapses to Internal with a fixed message, so a
// storage error never leaks its text to the caller.
func automationError(err error, fallback string) error {
	switch {
	case errors.Is(err, repository.ErrAutomationNotFound):
		return status.Error(codes.NotFound, "automation not found")
	case errors.Is(err, repository.ErrAutomationNameTaken):
		return status.Error(codes.AlreadyExists, "an automation with that name already exists")
	case errors.Is(err, repository.ErrAutomationNameEmpty):
		return status.Error(codes.InvalidArgument, "automation name is required")
	case errors.Is(err, repository.ErrAutomationNameTooLong):
		return status.Error(codes.InvalidArgument, "automation name is too long")
	case errors.Is(err, repository.ErrAutomationNoPrompt):
		return status.Error(codes.InvalidArgument, "automation prompt is required")
	case errors.Is(err, repository.ErrAutomationPromptLong):
		return status.Error(codes.InvalidArgument, "automation prompt is too long")
	case errors.Is(err, repository.ErrAutomationToolInvalid):
		return status.Error(codes.InvalidArgument, "an allowed tool needs both a server and a tool name")
	case errors.Is(err, repository.ErrAutomationTooManyTools):
		return status.Error(codes.InvalidArgument, "too many allowed tools")
	case errors.Is(err, repository.ErrAutomationIntegrationToolUnsupported):
		return status.Error(codes.FailedPrecondition, "integration tools are not available to automations")
	case errors.Is(err, repository.ErrAutomationMemoryToolUnsupported):
		return status.Error(codes.FailedPrecondition, "memory tools are not available to automations")
	case errors.Is(err, repository.ErrScheduleKindUnknown):
		return status.Error(codes.InvalidArgument, "schedule must be an interval or a daily time")
	case errors.Is(err, repository.ErrScheduleIntervalRange):
		return status.Error(codes.InvalidArgument, "interval must be between 1 minute and 7 days")
	case errors.Is(err, repository.ErrScheduleDailyMinute):
		return status.Error(codes.InvalidArgument, "daily time must be a minute of the day")
	default:
		return status.Error(codes.Internal, fallback)
	}
}

func scheduleFromProto(schedule *turingv1.AutomationSchedule) (repository.Schedule, error) {
	switch schedule.GetKind() {
	case turingv1.AutomationScheduleKind_AUTOMATION_SCHEDULE_KIND_INTERVAL:
		return repository.Schedule{
			Kind:     repository.ScheduleInterval,
			Interval: time.Duration(schedule.GetIntervalMinutes()) * time.Minute,
		}, nil
	case turingv1.AutomationScheduleKind_AUTOMATION_SCHEDULE_KIND_DAILY:
		return repository.Schedule{
			Kind:           repository.ScheduleDaily,
			DailyMinuteUTC: int(schedule.GetDailyMinuteUtc()),
		}, nil
	default:
		return repository.Schedule{}, status.Error(codes.InvalidArgument, "schedule must be an interval or a daily time")
	}
}

func toolsFromProto(tools []*turingv1.AutomationTool) []repository.AutomationTool {
	converted := make([]repository.AutomationTool, 0, len(tools))
	for _, tool := range tools {
		converted = append(converted, repository.AutomationTool{
			ServerName: tool.GetServerName(),
			ToolName:   tool.GetToolName(),
		})
	}
	return converted
}

func toProto(automation repository.Automation) *turingv1.Automation {
	tools := make([]*turingv1.AutomationTool, 0, len(automation.AllowedTools))
	for _, tool := range automation.AllowedTools {
		tools = append(tools, &turingv1.AutomationTool{ServerName: tool.ServerName, ToolName: tool.ToolName})
	}
	return &turingv1.Automation{
		AutomationId:              automation.AutomationID,
		Name:                      automation.Name,
		Prompt:                    automation.Prompt,
		Schedule:                  scheduleToProto(automation.Schedule),
		Enabled:                   automation.Enabled,
		AllowedTools:              tools,
		LastRunAt:                 parseTimestamp(automation.LastRunAt),
		NextRunAt:                 parseTimestamp(automation.NextDueAt),
		SessionId:                 automation.SessionID,
		LastRunId:                 automation.LastRunID,
		LastRunStatus:             automation.LastRunStatus,
		LastRunError:              automation.LastRunError,
		LastOccurrenceFailureCode: automation.LastOccurrenceFailureCode,
		LastOccurrenceFailedAt:    parseTimestamp(automation.LastOccurrenceFailedAt),
		CreatedAt:                 parseTimestamp(automation.CreatedAt),
		UpdatedAt:                 parseTimestamp(automation.UpdatedAt),
	}
}

func scheduleToProto(schedule repository.Schedule) *turingv1.AutomationSchedule {
	switch schedule.Kind {
	case repository.ScheduleInterval:
		return &turingv1.AutomationSchedule{
			Kind:            turingv1.AutomationScheduleKind_AUTOMATION_SCHEDULE_KIND_INTERVAL,
			IntervalMinutes: int32(schedule.Interval / time.Minute),
		}
	case repository.ScheduleDaily:
		return &turingv1.AutomationSchedule{
			Kind:           turingv1.AutomationScheduleKind_AUTOMATION_SCHEDULE_KIND_DAILY,
			DailyMinuteUtc: int32(schedule.DailyMinuteUTC),
		}
	default:
		return &turingv1.AutomationSchedule{}
	}
}

func parseTimestamp(value string) *timestamppb.Timestamp {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return nil
	}
	return timestamppb.New(parsed)
}
