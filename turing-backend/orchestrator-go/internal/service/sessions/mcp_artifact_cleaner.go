package sessions

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
)

type mcpArtifactCleaner struct {
	manifest              sandboxArtifactManifest
	endpoint              string
	approvalConsumerToken string
	client                *http.Client
}

// NewMCPArtifactCleaner withdraws the scratch files a session's runs left in
// the tool sandbox, and forgets exactly the sandbox manifest rows that describe
// them.
func NewMCPArtifactCleaner(
	manifest sandboxArtifactManifest,
	endpoint string,
	approvalConsumerToken string,
	client *http.Client,
) SessionArtifactCleaner {
	if client == nil {
		client = http.DefaultClient
	}
	return &mcpArtifactCleaner{
		manifest:              manifest,
		endpoint:              endpoint,
		approvalConsumerToken: approvalConsumerToken,
		client:                client,
	}
}

func (c *mcpArtifactCleaner) ArtifactScope() string { return ArtifactScopeSandbox }

// ForgetCleanedArtifacts drops the rows for the files the namespace removal
// just took with it, and only those. A legacy unowned artifact was never
// deleted, so its row stays: it is the record that a file the sandbox does not
// own is still on disk.
func (c *mcpArtifactCleaner) ForgetCleanedArtifacts(ctx context.Context, sessionID string) error {
	return forgetSandboxArtifacts(ctx, c.manifest, sessionID)
}

func forgetSandboxArtifacts(ctx context.Context, manifest sandboxArtifactManifest, sessionID string) error {
	artifacts, err := manifest.SessionSandboxArtifacts(ctx, sessionID)
	if err != nil {
		return err
	}
	for _, artifact := range artifacts {
		if artifact.Policy != repository.SandboxArtifactPolicyDeleteOnSessionDelete {
			continue
		}
		if err := manifest.DeleteSandboxArtifact(ctx, artifact.ArtifactID); err != nil {
			return err
		}
	}
	return nil
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
