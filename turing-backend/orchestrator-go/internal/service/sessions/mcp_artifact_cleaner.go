package sessions

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
)

type mcpArtifactCleaner struct {
	endpoint              string
	approvalConsumerToken string
	client                *http.Client
}

func NewMCPArtifactCleaner(endpoint string, approvalConsumerToken string, client *http.Client) sessionArtifactCleaner {
	if client == nil {
		client = http.DefaultClient
	}
	return &mcpArtifactCleaner{
		endpoint:              endpoint,
		approvalConsumerToken: approvalConsumerToken,
		client:                client,
	}
}

func (c *mcpArtifactCleaner) CleanupSessionArtifacts(ctx context.Context, sessionID string, lifecycleVersion int64) error {
	if c.endpoint == "" || c.approvalConsumerToken == "" {
		return errors.New("MCP artifact cleanup is not configured")
	}
	body, err := json.Marshal(map[string]any{
		"sessionId":        sessionID,
		"lifecycleVersion": lifecycleVersion,
	})
	if err != nil {
		return err
	}
	cleanupURL := strings.TrimSuffix(c.endpoint, "/mcp") + "/internal/session-cleanup"
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, cleanupURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("content-type", "application/json")
	request.Header.Set("authorization", "Bearer "+c.approvalConsumerToken)
	response, err := c.client.Do(request)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return errors.New("MCP artifact cleanup failed")
	}
	var result map[string]any
	decoder := json.NewDecoder(io.LimitReader(response.Body, 64*1024))
	decoder.UseNumber()
	if err := decoder.Decode(&result); err != nil {
		return errors.New("MCP artifact cleanup returned an invalid response")
	}
	if result == nil {
		return errors.New("MCP artifact cleanup failed")
	}
	if _, valid := result["namespaceRemoved"].(bool); !valid {
		return errors.New("MCP artifact cleanup returned an invalid response")
	}
	version, valid := result["lifecycleVersion"].(json.Number)
	if !valid {
		return errors.New("MCP artifact cleanup returned an invalid response")
	}
	gotVersion, err := version.Int64()
	if err != nil || gotVersion != lifecycleVersion {
		return errors.New("MCP artifact cleanup returned an invalid lifecycle version")
	}
	return nil
}
