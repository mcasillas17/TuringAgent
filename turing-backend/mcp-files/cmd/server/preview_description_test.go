package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mcasillas17/TuringAgent/turing-backend/approvalpreview"
)

func TestMutationDiscoveryDescribesEffectiveReviewLimit(t *testing.T) {
	for _, tool := range listTools() {
		if tool["name"] != "files.create" && tool["name"] != "files.update" {
			continue
		}
		description := tool["description"].(string)
		for _, phrase := range []string{"64 KiB", fmt.Sprintf("%d bytes", approvalpreview.MaxTextBytes), "before", "after", "approval"} {
			if !strings.Contains(description, phrase) {
				t.Errorf("%s description must disclose %q: %q", tool["name"], phrase, description)
			}
		}
	}
}
