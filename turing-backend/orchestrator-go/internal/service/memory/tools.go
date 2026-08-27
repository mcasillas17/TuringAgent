package memory

import (
	"context"
	"fmt"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/memoryfiles"
	toolpolicy "github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/tools"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
)

// maxMemoryQueryBytes mirrors the repository's own query bound — which counts
// bytes — so an over-long query is refused where the model can see why, rather
// than deep in FTS5.
const maxMemoryQueryBytes = 512

// maxBeliefIDBytes bounds the identity a read may name. An identity is a ULID
// with a short prefix; anything near this is not one.
const maxBeliefIDBytes = 128

// maxMemorySearchResults bounds one answer. Search is recall over the user's
// own memory, not an export of it, and a model that could ask for all of it
// would put the whole vault in one prompt.
const maxMemorySearchResults = 10

// schemaMaxBytesKeyword carries a byte budget in a descriptor whose only
// length keyword counts characters.
//
// JSON Schema's maxLength is a count of code points, and three of these
// arguments are bounded in bytes by the layers underneath. Advertising 16384
// as maxLength would tell a model it may send 16384 characters of Japanese —
// three times what the runtime will accept — so the byte number goes in a
// vendor extension the schema explicitly permits, and the description states
// the unit in the same words the refusal will.
const schemaMaxBytesKeyword = "x-turing-maxBytes"

type memoryTool struct {
	name        string
	description string
	// arguments is the complete set of keys this tool accepts. Dispatch checks
	// against it directly rather than trusting the schema to have been read:
	// the schema is documentation for the model, and the model is the party
	// this bound exists to constrain.
	arguments []string
	required  []string
	schema    map[string]any
}

var memoryTools = []memoryTool{
	{
		name: ToolSearch,
		description: "Search what Turing remembers about this user. Takes a query and nothing else: " +
			"there is no folder, path or scope to aim it at, and it never returns unreviewed proposals.",
		arguments: []string{"query"},
		required:  []string{"query"},
		schema: objectSchema([]string{"query"}, map[string]any{
			"query": map[string]any{
				"type": "string",
				"description": fmt.Sprintf(
					"What to look for in accepted memory. At most %d bytes of UTF-8.", maxMemoryQueryBytes),
				"minLength":           1,
				schemaMaxBytesKeyword: maxMemoryQueryBytes,
			},
		}),
	},
	{
		name: ToolRead,
		description: "Read one accepted memory in full, by the stable id a search returned. " +
			"The answer is read from the file each time, so it reflects any edit the user has made.",
		arguments: []string{"belief_id"},
		required:  []string{"belief_id"},
		schema: objectSchema([]string{"belief_id"}, map[string]any{
			"belief_id": map[string]any{
				"type": "string",
				"description": fmt.Sprintf(
					"The stable id of an accepted memory, as returned by memory.search. At most %d bytes of UTF-8.",
					maxBeliefIDBytes),
				"minLength":           1,
				schemaMaxBytesKeyword: maxBeliefIDBytes,
			},
		}),
	},
	{
		name: ToolRemember,
		description: "Propose something for the user to remember. It is written to their inbox for " +
			"review and becomes memory only if they accept it. There is no destination to choose.",
		arguments: []string{"title", "body", "kind"},
		required:  []string{"title", "body"},
		schema: objectSchema([]string{"title", "body"}, map[string]any{
			// The title is the one argument genuinely bounded in characters —
			// the repository and the vault both count runes when they check it
			// and when they build a filename from it — so it is the one
			// argument where maxLength says what is actually enforced.
			"title": map[string]any{
				"type": "string",
				"description": fmt.Sprintf(
					"A short name for the proposal, in the user's own terms. At most %d characters.",
					maxMemoryTitleRunes),
				"minLength": 1,
				"maxLength": maxMemoryTitleRunes,
			},
			"body": map[string]any{
				"type": "string",
				"description": fmt.Sprintf(
					"The complete claim being proposed. At most %d bytes of UTF-8; it is refused, never shortened.",
					memoryfiles.MaxCandidateBodyBytes),
				"minLength":           1,
				schemaMaxBytesKeyword: memoryfiles.MaxCandidateBodyBytes,
			},
			"kind": map[string]any{
				"type": "string",
				"enum": []any{
					string(memoryfiles.KindBelief),
					string(memoryfiles.KindProfileEdit),
				},
				"description": "belief for a fact about the user, profile_edit to propose a change to their profile. Defaults to belief.",
			},
		}),
	},
}

func objectSchema(required []string, properties map[string]any) map[string]any {
	requiredValues := make([]any, len(required))
	for index, value := range required {
		requiredValues[index] = value
	}
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             requiredValues,
		"properties":           properties,
	}
}

func lookupMemoryTool(name string) (memoryTool, bool) {
	for _, tool := range memoryTools {
		if tool.name == name {
			return tool, true
		}
	}
	return memoryTool{}, false
}

// ListMemoryTools is the internal-facet discovery call the runtime uses to wire
// memory tools dynamically.
//
// A toggle that is off returns an empty list rather than a disabled one. The
// runtime's registry filter drops what is not listed, so this — plus the
// registry-change notification the toggle publishes — is what takes memory away
// from a worker that is already connected, with no restart.
func (s *Server) ListMemoryTools(ctx context.Context, _ *turingv1.ListMemoryToolsRequest) (*turingv1.ListMemoryToolsResponse, error) {
	enabled, err := s.repo.MemoryEnabled(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, "read memory settings failed")
	}
	response := &turingv1.ListMemoryToolsResponse{Tools: make([]*turingv1.MemoryToolDescriptor, 0, len(memoryTools))}
	if !enabled || s.vault == nil {
		return response, nil
	}
	for _, tool := range memoryTools {
		available, err := s.repo.PseudoServerToolAvailable(ctx, ServerName, tool.name)
		if err != nil {
			return nil, status.Error(codes.Internal, "list memory tools failed")
		}
		if !available {
			continue
		}
		policy, found, err := s.repo.PseudoServerToolPolicy(ctx, ServerName, tool.name)
		if err != nil {
			return nil, status.Error(codes.Internal, "list memory tools failed")
		}
		if !found {
			policy = string(toolpolicy.DefaultPolicyFor(ServerName, tool.name))
		}
		schema, err := structpb.NewStruct(tool.schema)
		if err != nil {
			return nil, status.Error(codes.Internal, "build memory tool schema failed")
		}
		response.Tools = append(response.Tools, &turingv1.MemoryToolDescriptor{
			ToolName:    tool.name,
			Policy:      memoryPolicyProto(policy),
			Schema:      schema,
			Enabled:     true,
			Description: tool.description,
		})
	}
	return response, nil
}

func memoryPolicyProto(policy string) turingv1.ToolPolicy {
	switch policy {
	case "safe":
		return turingv1.ToolPolicy_TOOL_POLICY_SAFE
	case "disabled":
		return turingv1.ToolPolicy_TOOL_POLICY_DISABLED
	default:
		return turingv1.ToolPolicy_TOOL_POLICY_APPROVAL_REQUIRED
	}
}
