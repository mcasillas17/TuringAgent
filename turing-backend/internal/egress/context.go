package egress

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
)

const DecisionVersion = 1

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
