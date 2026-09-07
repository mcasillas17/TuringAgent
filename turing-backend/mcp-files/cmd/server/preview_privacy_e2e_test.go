//go:build sqlite_fts5

package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/approvalpreview"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/testkit"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

func TestRealCredentialPreviewNeverDisclosesOrRetainsSecrets(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "privacy.db")
	app, err := testkit.NewApp(testkit.Config{ClientAPIKey: "client", RuntimeToken: "runtime", ApprovalConsumerToken: "consumer",
		ApprovalJWTSecret: "secret", DatabasePath: dbPath, FilesMCPEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Stop)
	serve := func(server *grpc.Server) string {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = listener.Close() })
		go func() { _ = server.Serve(listener) }()
		return listener.Addr().String()
	}
	internalAddr, publicAddr := serve(app.InternalServer), serve(app.PublicServer)
	files := httptest.NewServer(newHandler(serverConfig{filesToken: "worker", cleanupToken: "cleanup", approvalJwtSecret: "secret",
		approvalConsumerToken: "consumer", orchestratorGRPCAddr: internalAddr, sandboxRoot: root}))
	t.Cleanup(files.Close)
	app.SetPreviewEndpoint(files.URL)
	conn, err := grpc.NewClient(publicAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	public := turingv1.NewApprovalServiceClient(conn)
	ctx := metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer client")
	database, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	const credential = "dXNlcjpmaXh0dXJl"
	for _, tt := range []struct {
		name, path, body string
		state            turingv1.ApprovalPreviewState
	}{
		{"docker", ".docker/config.json", `{"auths":{"registry.example":{"auth":"` + credential + `"}}}`, turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_REDACTED},
		{"auth-json", "note.txt", `{"auths":{"registry.example":{"auth":"` + credential + `"}}}`, turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_REDACTED},
		{"cookie", "note.txt", "Cookie: sid=" + credential + "\n", turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_REDACTED},
		{"set-cookie", "note.txt", "Set-Cookie: sid=" + credential + "; Secure\n", turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_REDACTED},
		{"composite-secret", "settings.ini", "aws_secret_access_key=" + credential + "\n", turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_REDACTED},
		{"binary-secret", "settings.ini", "aws_secret_access_key=" + credential + "\x00", turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_BINARY},
		{"oversized-secret", "settings.ini", "aws_secret_access_key=" + credential + strings.Repeat("x", approvalpreview.MaxArgsBytes+1), turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_OVERSIZED},
	} {
		for _, location := range []string{"before-only", "after-only"} {
			t.Run(tt.name+"/"+location, func(t *testing.T) {
				logical := filepath.ToSlash(filepath.Join(tt.name, location, tt.path))
				physical := filepath.Join(root, logical)
				before, after := "harmless before\n", "harmless replacement\n"
				if location == "before-only" {
					before = tt.body
				} else {
					after = tt.body
				}
				if err := os.MkdirAll(filepath.Dir(physical), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(physical, []byte(before), 0600); err != nil {
					t.Fatal(err)
				}
				fixture, err := app.CreateFileApproval(context.Background(), "files.update", map[string]any{"path": logical, "content": after})
				if err != nil {
					t.Fatal(err)
				}
				d, err := public.GetApprovalDetails(ctx, &turingv1.GetApprovalDetailsRequest{ApprovalId: fixture.ApprovalID})
				if err != nil {
					t.Fatal(err)
				}
				raw, err := json.Marshal(d)
				if err != nil {
					t.Fatal(err)
				}
				var retained string
				if err := database.QueryRow(`SELECT snapshot_json FROM approval_previews WHERE approval_id=?`, fixture.ApprovalID).Scan(&retained); err != nil {
					t.Fatal(err)
				}
				if d.CanApprove || d.PreviewState != tt.state || d.PreviewHash == "" || d.PreviewHash == fixture.ArgsHash ||
					d.FilePreview != nil || d.ArgsHash != "" || strings.Contains(string(raw), credential) || strings.Contains(retained, credential) ||
					strings.Contains(string(raw), fixture.ArgsHash) || strings.Contains(retained, fixture.ArgsHash) {
					t.Fatalf("credential preview disclosed or retained sensitive data; state=%s", d.PreviewState)
				}
			})
		}
	}
}
