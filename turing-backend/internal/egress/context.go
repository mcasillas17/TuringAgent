package egress

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strings"
	"unicode"
)

const (
	DecisionVersion          = 2
	maxSkillDisplayNameRunes = 80
)

type SkillSnapshot struct {
	SkillID             string            `json:"skill_id"`
	Name                string            `json:"name"`
	Description         string            `json:"description"`
	Category            string            `json:"category"`
	Instructions        string            `json:"instructions"`
	References          map[string]string `json:"references"`
	Withheld            bool              `json:"withheld"`
	MissingCapabilities []string          `json:"missing_capabilities"`
}

func SkillSnapshotFingerprint(snapshots []SkillSnapshot) (string, error) {
	canonical := make([]SkillSnapshot, len(snapshots))
	for index, snapshot := range snapshots {
		canonical[index] = snapshot
		canonical[index].References = cloneStringMap(snapshot.References)
		canonical[index].MissingCapabilities = append(
			[]string(nil),
			snapshot.MissingCapabilities...,
		)
		slices.Sort(canonical[index].MissingCapabilities)
	}
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func SanitizeSkillDisplayName(displayName, skillID string) string {
	sanitized := sanitizeSkillDisplayText(displayName)
	if isEmptySkillDisplayName(sanitized) {
		sanitized = sanitizeSkillDisplayText(skillID)
	}
	if isEmptySkillDisplayName(sanitized) {
		sanitized = "(unnamed)"
	}
	runes := []rune(sanitized)
	if len(runes) > maxSkillDisplayNameRunes {
		return string(runes[:maxSkillDisplayNameRunes-1]) + "…"
	}
	return sanitized
}

// skillDisplayNameEmptyCutset ends with a space on purpose: input arrives from
// sanitizeSkillDisplayText already whitespace-collapsed, so trimming single
// spaces alongside path separators is what lets "/ /" count as empty.
const skillDisplayNameEmptyCutset = `/\ `

func isEmptySkillDisplayName(value string) bool {
	return value == "" || strings.Trim(value, skillDisplayNameEmptyCutset) == ""
}

func sanitizeSkillDisplayText(value string) string {
	var flattened strings.Builder
	for _, current := range value {
		switch {
		case unicode.IsSpace(current):
			flattened.WriteRune(' ')
		case unicode.IsControl(current), unicode.In(current, unicode.Cf):
			continue
		default:
			flattened.WriteRune(current)
		}
	}
	return strings.Join(strings.Fields(flattened.String()), " ")
}

func HashCredentialReference(reference string) string {
	sum := sha256.Sum256([]byte(reference))
	return hex.EncodeToString(sum[:])
}

func cloneStringMap(source map[string]string) map[string]string {
	if len(source) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(source))
	for key, value := range source {
		cloned[key] = value
	}
	return cloned
}
