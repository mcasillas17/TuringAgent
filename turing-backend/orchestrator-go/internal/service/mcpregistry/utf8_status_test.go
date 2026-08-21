package mcpregistry

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestBoundedStatusMessagePreservesUTF8(t *testing.T) {
	message := "error: " + strings.Repeat("é", 400)
	bounded := boundedStatusMessage(message)
	if len(bounded) > maxMCPStatusMessageBytes || !utf8.ValidString(bounded) {
		t.Fatalf("bounded message bytes=%d valid=%v", len(bounded), utf8.ValidString(bounded))
	}
}
