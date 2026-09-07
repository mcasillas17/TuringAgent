//go:build sqlite_fts5

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/testkit"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestRealReviewedFileMutationEndToEnd(t *testing.T) {
	for _, scenario := range []string{"create", "update", "stale before decision", "stale after decision", "collision refresh"} {
		t.Run(scenario, func(t *testing.T) {
			root := t.TempDir()
			app, err := testkit.NewApp(testkit.Config{ClientAPIKey: "client", RuntimeToken: "runtime", ApprovalConsumerToken: "consumer", ApprovalJWTSecret: "secret",
				DatabasePath: filepath.Join(t.TempDir(), "test.db"), FilesMCPEnabled: true})
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
			internalAddr := serve(app.InternalServer)
			publicAddr := serve(app.PublicServer)
			files := httptest.NewServer(newHandler(serverConfig{filesToken: "worker", cleanupToken: "cleanup", approvalJwtSecret: "secret", approvalConsumerToken: "consumer", orchestratorGRPCAddr: internalAddr, sandboxRoot: root}))
			t.Cleanup(files.Close)
			app.SetPreviewEndpoint(files.URL)
			dial := func(addr string) *grpc.ClientConn {
				conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = conn.Close() })
				return conn
			}
			public := turingv1.NewApprovalServiceClient(dial(publicAddr))
			internal := turingv1.NewApprovalServiceClient(dial(internalAddr))
			clientCtx := metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer client")
			runtimeCtx := metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer runtime")
			tool := "files.update"
			if scenario == "create" || scenario == "collision refresh" {
				tool = "files.create"
			} else {
				if err := os.WriteFile(filepath.Join(root, "note.txt"), []byte("before\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			args := map[string]any{"path": "note.txt", "content": "reviewed <&> café\n"}
			fixture, err := app.CreateFileApproval(context.Background(), tool, args)
			if err != nil {
				t.Fatal(err)
			}
			collision := filepath.Join(root, "sessions", fixture.SessionID, "runs", fixture.RunID, "files", "note.txt")
			if scenario == "collision refresh" {
				if err := os.MkdirAll(filepath.Dir(collision), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(collision, []byte("collision"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := public.GetApprovalDetails(context.Background(), &turingv1.GetApprovalDetailsRequest{ApprovalId: fixture.ApprovalID}); status.Code(err) != codes.Unauthenticated {
				t.Fatalf("unauth detail=%v", err)
			}
			if _, err := internal.GetApprovalDetails(runtimeCtx, &turingv1.GetApprovalDetailsRequest{ApprovalId: fixture.ApprovalID}); status.Code(err) != codes.PermissionDenied {
				t.Fatalf("internal detail=%v", err)
			}
			d, err := public.GetApprovalDetails(clientCtx, &turingv1.GetApprovalDetailsRequest{ApprovalId: fixture.ApprovalID})
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "collision refresh" {
				if d.CanApprove || d.PreviewState != turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_STALE {
					t.Fatalf("collision preview=%s", d.PreviewState)
				}
				if err := os.Remove(collision); err != nil {
					t.Fatal(err)
				}
				ordinary, err := public.GetApprovalDetails(clientCtx, &turingv1.GetApprovalDetailsRequest{ApprovalId: fixture.ApprovalID})
				if err != nil {
					t.Fatal(err)
				}
				if ordinary.CanApprove || ordinary.PreviewHash != d.PreviewHash {
					t.Fatal("collision recovery silently refreshed")
				}
				d, err = public.GetApprovalDetails(clientCtx, &turingv1.GetApprovalDetailsRequest{ApprovalId: fixture.ApprovalID, RefreshPreview: true})
				if err != nil {
					t.Fatal(err)
				}
				pending, err := internal.GetApprovalForRuntime(runtimeCtx, &turingv1.GetApprovalForRuntimeRequest{ApprovalId: fixture.ApprovalID})
				if err != nil {
					t.Fatal(err)
				}
				if pending.Status != turingv1.ApprovalStatus_APPROVAL_STATUS_PENDING || pending.ApprovalToken != "" {
					t.Fatal("refresh approved without a decision")
				}
			}
			if !d.CanApprove || d.ArgsHash != fixture.ArgsHash {
				t.Fatalf("details=%+v", d)
			}
			if d.ArgumentsJson != "{}" {
				t.Fatal("READY public file details duplicated canonical argument content")
			}
			target := filepath.Join(root, d.FilePreview.PhysicalPath)
			if scenario == "stale before decision" {
				if err := os.WriteFile(target, []byte("changed\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			_, err = public.ApproveApproval(clientCtx, &turingv1.ApproveApprovalRequest{ApprovalId: fixture.ApprovalID, PreviewHash: d.PreviewHash, ArgsHash: d.ArgsHash})
			if scenario == "stale before decision" {
				if status.Code(err) != codes.FailedPrecondition {
					t.Fatalf("stale decision=%v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			decision, err := internal.GetApprovalForRuntime(runtimeCtx, &turingv1.GetApprovalForRuntimeRequest{ApprovalId: fixture.ApprovalID})
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "stale after decision" {
				if err := os.WriteFile(target, []byte("changed\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			raw, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{
				"name": tool, "arguments": args, "_meta": map[string]any{"approvalToken": decision.ApprovalToken, "provenanceToken": fixture.ProvenanceToken}}})
			if err != nil {
				t.Fatal(err)
			}
			request, err := http.NewRequest(http.MethodPost, files.URL+"/mcp", bytes.NewReader(raw))
			if err != nil {
				t.Fatal(err)
			}
			request.Header.Set("Authorization", "Bearer worker")
			response, err := files.Client().Do(request)
			if err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(response.Body)
			_ = response.Body.Close()
			if err != nil {
				t.Fatal(err)
			}
			var result map[string]any
			if err := json.Unmarshal(body, &result); err != nil {
				t.Fatal(err)
			}
			actual, err := os.ReadFile(target)
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "stale after decision" {
				if result["error"] == nil || string(actual) != "changed\n" {
					t.Fatalf("stale write: %s bytes %q", body, actual)
				}
			} else if result["error"] != nil || string(actual) != d.FilePreview.AfterText {
				t.Fatalf("write not reviewed operation: %s bytes %q", body, actual)
			}
		})
	}
}
