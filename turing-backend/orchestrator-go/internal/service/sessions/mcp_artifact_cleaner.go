package sessions

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

type mcpArtifactCleaner struct {
	endpoint      string
	filesToken    string
	internalToken string
	client        *http.Client
}

func NewMCPArtifactCleaner(endpoint string, filesToken string, internalToken string, client *http.Client) sessionArtifactCleaner {
	if client == nil {
		client = http.DefaultClient
	}
	return &mcpArtifactCleaner{
		endpoint:      endpoint,
		filesToken:    filesToken,
		internalToken: internalToken,
		client:        client,
	}
}

func (c *mcpArtifactCleaner) CleanupSessionArtifacts(ctx context.Context, sessionID string, lifecycleVersion int64) error {
	if c.endpoint == "" || c.filesToken == "" || c.internalToken == "" {
		return errors.New("MCP artifact cleanup is not configured")
	}
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "files.session_cleanup",
			"arguments": map[string]any{
				"sessionId":        sessionID,
				"lifecycleVersion": lifecycleVersion,
			},
			"_meta": map[string]any{
				"internalCleanupToken": c.internalToken,
			},
		},
	})
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("content-type", "application/json")
	request.Header.Set("authorization", "Bearer "+c.filesToken)
	response, err := c.client.Do(request)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return errors.New("MCP artifact cleanup failed")
	}
	var envelope struct {
		JSONRPC string         `json:"jsonrpc"`
		ID      json.Number    `json:"id"`
		Result  map[string]any `json:"result"`
		Error   any            `json:"error"`
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 64*1024))
	decoder.UseNumber()
	if err := decoder.Decode(&envelope); err != nil {
		return errors.New("MCP artifact cleanup returned an invalid response")
	}
	if envelope.JSONRPC != "2.0" || envelope.Error != nil || envelope.Result == nil {
		return errors.New("MCP artifact cleanup failed")
	}
	if _, valid := envelope.Result["namespaceRemoved"].(bool); !valid {
		return errors.New("MCP artifact cleanup returned an invalid response")
	}
	version, valid := envelope.Result["lifecycleVersion"].(json.Number)
	if !valid {
		return errors.New("MCP artifact cleanup returned an invalid response")
	}
	gotVersion, err := version.Int64()
	if err != nil || gotVersion != lifecycleVersion {
		return errors.New("MCP artifact cleanup returned an invalid lifecycle version")
	}
	return nil
}
