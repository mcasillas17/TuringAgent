// Package teamfiles reads the user's team/<id>/AGENT.md specialist profiles.
// Like skills, a profile lives in a file and its folder name is its identity;
// SQLite holds only the user's enablement and grant.
package teamfiles

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"gopkg.in/yaml.v3"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/rootedfile"
)

const (
	agentFileName       = "AGENT.md"
	maxAgentFileBytes   = 64 * 1024
	maxNameRunes        = 120
	maxDescriptionRunes = 500
	maxEmojiRunes       = 16
	maxPatternRunes     = 128
	maxModelRunes       = 128
	revisionFormat      = "turing.agent-profile.v1"

	// ReservedID is the orchestrator's own name; no file may claim it.
	ReservedID = "turing"
)

const (
	MemoryNone    = "none"
	MemoryRead    = "read"
	MemoryPropose = "propose"
)

var profileIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{1,31}$`)

type Profile struct {
	ID           string
	Name         string
	Emoji        string
	Description  string
	Version      string
	Model        string
	Tools        []string
	Skills       []string
	Requires     []string
	Memory       string
	MaxToolCalls int
	Instructions string
	// Revision hashes the authority fields only, so a grant survives edits to
	// the instructions and display fields. Empty whenever ParseError is set.
	Revision   string
	ParseError string
}

type Store struct {
	root string
}

func New(root string) *Store {
	return &Store{root: filepath.Clean(root)}
}

// Scan reads every profile folder directly under the root. A missing root is
// an empty team; a profile that fails to load is returned with ParseError set
// so one bad file never hides the others.
func (s *Store) Scan() ([]Profile, error) {
	info, err := os.Lstat(s.root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("inspect team root: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, errors.New("team root must be a real directory")
	}
	entries, err := os.ReadDir(s.root)
	if err != nil {
		return nil, fmt.Errorf("scan team root: %w", err)
	}
	profiles := make([]Profile, 0, len(entries))
	for _, entry := range entries {
		id := entry.Name()
		if strings.HasPrefix(id, ".") {
			continue
		}
		if entry.Type()&os.ModeSymlink != 0 {
			profiles = append(profiles, Profile{ID: displayID(id), ParseError: "profile folder must be a real directory, not a symlink"})
			continue
		}
		if !entry.IsDir() {
			continue
		}
		profile, found := s.load(id)
		if found {
			// Such a name fails the ID pattern, so it only ever names a
			// parse error; it must still survive the trip as a protobuf string.
			profile.ID = displayID(id)
			profile.ParseError = strings.ToValidUTF8(profile.ParseError, "\uFFFD")
			profiles = append(profiles, profile)
		}
	}
	slices.SortFunc(profiles, func(a, b Profile) int { return strings.Compare(a.ID, b.ID) })
	return profiles, nil
}

// displayID is the folder name as the API carries it. A name that is not UTF-8
// cannot be a protobuf string, so it is quoted; quoting keeps two such names
// apart, where replacing their bad bytes could make them collide.
func displayID(name string) string {
	if utf8.ValidString(name) {
		return name
	}
	return strconv.Quote(name)
}

// withoutPath drops the path an fs.PathError carries: a parse error is shown
// to the user as written, and the path is the container mount (/team/...),
// not anywhere on their machine.
func withoutPath(err error) error {
	var pathErr *fs.PathError
	if errors.As(err, &pathErr) {
		return pathErr.Err
	}
	return err
}

func (s *Store) load(id string) (Profile, bool) {
	filename := filepath.Join(s.root, id, agentFileName)
	info, err := os.Lstat(filename)
	if errors.Is(err, os.ErrNotExist) {
		return Profile{}, false
	}
	profile := Profile{ID: id}
	if err != nil {
		profile.ParseError = fmt.Sprintf("inspect %s: %v", agentFileName, withoutPath(err))
		return profile, true
	}
	if !profileIDPattern.MatchString(id) {
		profile.ParseError = "profile folder name must match ^[a-z][a-z0-9-]{1,31}$"
		return profile, true
	}
	if id == ReservedID {
		profile.ParseError = fmt.Sprintf("%q is reserved for the orchestrator", ReservedID)
		return profile, true
	}
	data, err := rootedfile.ReadBoundedRegular(s.root, filename, maxAgentFileBytes, info, "team root")
	if err != nil {
		profile.ParseError = fmt.Sprintf("read %s: %v", agentFileName, withoutPath(err))
		return profile, true
	}
	parsed, err := parse(id, data)
	if err != nil {
		profile.ParseError = err.Error()
		return profile, true
	}
	return parsed, true
}

type frontmatter struct {
	Name         string        `yaml:"name"`
	Emoji        string        `yaml:"emoji"`
	Description  string        `yaml:"description"`
	Version      string        `yaml:"version"`
	Model        string        `yaml:"model"`
	Tools        []string      `yaml:"tools"`
	Skills       []string      `yaml:"skills"`
	Memory       string        `yaml:"memory"`
	Requires     []string      `yaml:"requires"`
	MaxToolCalls toolCallLimit `yaml:"max_tool_calls"`
}

// toolCallLimit is an integer the file wrote as one. yaml.v3 would otherwise
// truncate 12.5 to 12 and -0.5 to 0, so a declaration nobody meant would be
// granted.
type toolCallLimit int

func (limit *toolCallLimit) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.ScalarNode || node.ShortTag() != "!!int" {
		return errors.New("frontmatter max_tool_calls must be a whole number")
	}
	var value int
	if err := node.Decode(&value); err != nil {
		return err
	}
	*limit = toolCallLimit(value)
	return nil
}

func parse(id string, data []byte) (Profile, error) {
	if !utf8.Valid(data) {
		return Profile{}, fmt.Errorf("%s must be UTF-8 text", agentFileName)
	}
	yamlText, body, err := rootedfile.SplitFrontmatter(string(data), agentFileName)
	if err != nil {
		return Profile{}, err
	}
	decoder := yaml.NewDecoder(strings.NewReader(yamlText))
	decoder.KnownFields(true)
	var metadata frontmatter
	if err := decoder.Decode(&metadata); err != nil {
		return Profile{}, fmt.Errorf("parse frontmatter: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return Profile{}, errors.New("frontmatter must contain one YAML document")
		}
		return Profile{}, fmt.Errorf("parse frontmatter: %w", err)
	}
	// The file is UTF-8, but a YAML !!binary scalar decodes to arbitrary
	// bytes, which no protobuf string field can carry.
	for _, value := range slices.Concat(
		[]string{metadata.Name, metadata.Emoji, metadata.Description, metadata.Version, metadata.Model, metadata.Memory},
		metadata.Tools, metadata.Skills, metadata.Requires,
	) {
		if !utf8.ValidString(value) {
			return Profile{}, errors.New("frontmatter values must be UTF-8 text")
		}
	}

	profile := Profile{
		ID:           id,
		Name:         strings.TrimSpace(metadata.Name),
		Emoji:        strings.TrimSpace(metadata.Emoji),
		Description:  strings.TrimSpace(metadata.Description),
		Version:      strings.TrimSpace(metadata.Version),
		Model:        strings.TrimSpace(metadata.Model),
		Memory:       strings.TrimSpace(metadata.Memory),
		MaxToolCalls: int(metadata.MaxToolCalls),
		Instructions: strings.TrimSpace(body),
	}
	switch {
	case profile.Name == "":
		return Profile{}, errors.New("frontmatter name is required")
	case profile.Description == "":
		return Profile{}, errors.New("frontmatter description is required")
	case utf8.RuneCountInString(profile.Name) > maxNameRunes:
		return Profile{}, fmt.Errorf("frontmatter name exceeds %d characters", maxNameRunes)
	case utf8.RuneCountInString(profile.Description) > maxDescriptionRunes:
		return Profile{}, fmt.Errorf("frontmatter description exceeds %d characters", maxDescriptionRunes)
	case utf8.RuneCountInString(profile.Emoji) > maxEmojiRunes:
		return Profile{}, fmt.Errorf("frontmatter emoji exceeds %d characters", maxEmojiRunes)
	case profile.MaxToolCalls < 0:
		return Profile{}, errors.New("frontmatter max_tool_calls must not be negative")
	case profile.MaxToolCalls > math.MaxInt32:
		return Profile{}, fmt.Errorf("frontmatter max_tool_calls must be at most %d", math.MaxInt32)
	}
	if profile.Model != "" && (utf8.RuneCountInString(profile.Model) > maxModelRunes || strings.IndexFunc(profile.Model, invalidPatternRune) >= 0) {
		return Profile{}, fmt.Errorf("frontmatter model %q is invalid", profile.Model)
	}
	switch profile.Memory {
	case "":
		profile.Memory = MemoryNone
	case MemoryNone, MemoryRead, MemoryPropose:
	default:
		return Profile{}, fmt.Errorf("frontmatter memory %q must be none, read or propose", profile.Memory)
	}
	if profile.Tools, err = normalizePatterns("tools", metadata.Tools); err != nil {
		return Profile{}, err
	}
	if profile.Skills, err = normalizePatterns("skills", metadata.Skills); err != nil {
		return Profile{}, err
	}
	if profile.Requires, err = normalizePatterns("requires", metadata.Requires); err != nil {
		return Profile{}, err
	}
	profile.Revision, err = revision(profile)
	if err != nil {
		return Profile{}, err
	}
	return profile, nil
}

// MatchPattern reports whether name matches an exact pattern or a trailing-*
// prefix pattern.
func MatchPattern(pattern, name string) bool {
	if prefix, wildcard := strings.CutSuffix(pattern, "*"); wildcard {
		return strings.HasPrefix(name, prefix)
	}
	return pattern == name
}

func normalizePatterns(field string, values []string) ([]string, error) {
	patterns := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		star := strings.IndexByte(value, '*')
		switch {
		case value == "", value == "*",
			utf8.RuneCountInString(value) > maxPatternRunes,
			strings.IndexFunc(value, invalidPatternRune) >= 0,
			star >= 0 && star != len(value)-1:
			return nil, fmt.Errorf("frontmatter %s pattern %q must be an exact name or a prefix ending in *", field, value)
		}
		// Tool patterns name the tool without its server; skill IDs are
		// folder paths and keep their slashes.
		if field != "skills" {
			if server, tool, qualified := strings.Cut(value, "/"); qualified {
				return nil, fmt.Errorf("frontmatter %s pattern %q names its server %q: write %s", field, value, server, tool)
			}
		}
		patterns = append(patterns, value)
	}
	slices.Sort(patterns)
	return slices.Compact(patterns), nil
}

func invalidPatternRune(r rune) bool {
	return unicode.IsSpace(r) || unicode.IsControl(r)
}

func revision(profile Profile) (string, error) {
	canonical, err := json.Marshal(struct {
		Format       string   `json:"format"`
		Tools        []string `json:"tools"`
		Skills       []string `json:"skills"`
		Memory       string   `json:"memory"`
		Model        string   `json:"model"`
		Requires     []string `json:"requires"`
		MaxToolCalls int      `json:"max_tool_calls"`
	}{revisionFormat, profile.Tools, profile.Skills, profile.Memory, profile.Model, profile.Requires, profile.MaxToolCalls})
	if err != nil {
		return "", fmt.Errorf("hash profile declaration: %w", err)
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}
