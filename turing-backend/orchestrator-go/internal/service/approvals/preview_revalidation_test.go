package approvals

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/approvalpreview"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func previewReadFixture(t *testing.T, handler http.HandlerFunc) (*approvalHarness, string) {
	t.Helper()
	h := newApprovalHarness(t)
	run := h.createRunningToolCall(t)
	args := map[string]any{"path": "note.txt", "content": "after"}
	raw, hash, err := canonicalArgs(args)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.database.Exec(`UPDATE tool_calls SET args_json=?,args_hash=? WHERE id='call_1'`, raw, hash); err != nil {
		t.Fatal(err)
	}
	id, err := h.service.CreateApprovalForTool(context.Background(), run.RunID, "call_1", "general_assistant", "files.update", args)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	h.service.SetPreviewEndpoint(server.URL)
	return h, id
}

func readyReadSnapshot(before string) approvalpreview.Snapshot {
	return approvalpreview.Snapshot{State: turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_READY,
		ArgumentsJSON: `{"content":"after","path":"note.txt"}`, File: &turingv1.FileMutationPreview{LogicalPath: "note.txt", PhysicalPath: "note.txt",
			Operation: "update", BeforeExists: true, BeforeText: before, BeforeHash: approvalpreview.Hash(before), AfterText: "after", AfterHash: approvalpreview.Hash("after")}}
}

func readPreviewDetails(t *testing.T, h *approvalHarness, id string, refresh bool) *turingv1.ApprovalDetails {
	t.Helper()
	d, err := h.service.GetApprovalDetails(context.Background(), &turingv1.GetApprovalDetailsRequest{ApprovalId: id, RefreshPreview: refresh})
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestFreshPreviewDoesOnePreparationButDecisionsStillRevalidate(t *testing.T) {
	var calls atomic.Int32
	h, id := previewReadFixture(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_ = json.NewEncoder(w).Encode(readyReadSnapshot("before"))
	})
	for index, refresh := range []bool{false, false, true} {
		d := readPreviewDetails(t, h, id, refresh)
		if !d.CanApprove {
			t.Fatalf("details not ready: %s", d.PreviewState)
		}
		if got := calls.Load(); got != int32(index+1) {
			t.Errorf("read %d: preparations=%d, want %d", index, got, index+1)
		}
	}
	d := readPreviewDetails(t, h, id, false)
	before := calls.Load()
	if _, err := h.service.ApproveApproval(context.Background(), &turingv1.ApproveApprovalRequest{ApprovalId: id, PreviewHash: d.PreviewHash, ArgsHash: d.ArgsHash}); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != before+1 {
		t.Fatal("approval did not perform exactly one live revalidation")
	}
}

func TestUnavailablePendingPreviewRecoversOnlyByExplicitRefresh(t *testing.T) {
	for _, initial := range []string{"transport failure", "collision"} {
		t.Run(initial, func(t *testing.T) {
			var recovered atomic.Bool
			var calls atomic.Int32
			h, id := previewReadFixture(t, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if !recovered.Load() {
					if initial == "transport failure" {
						http.Error(w, "unavailable", http.StatusServiceUnavailable)
						return
					}
					_ = json.NewEncoder(w).Encode(approvalpreview.Snapshot{State: turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_STALE, ArgumentsJSON: "{}"})
					return
				}
				_ = json.NewEncoder(w).Encode(readyReadSnapshot("recovered"))
			})
			first := readPreviewDetails(t, h, id, false)
			if first.CanApprove {
				t.Fatal("failed preparation enabled approval")
			}
			recovered.Store(true)
			ordinary := readPreviewDetails(t, h, id, false)
			if ordinary.CanApprove || ordinary.PreviewHash != first.PreviewHash || calls.Load() != 1 {
				t.Fatal("ordinary read silently refreshed unavailable snapshot")
			}
			if _, err := h.service.ApproveApproval(context.Background(), &turingv1.ApproveApprovalRequest{ApprovalId: id, PreviewHash: first.PreviewHash, ArgsHash: first.ArgsHash}); status.Code(err) != codes.FailedPrecondition {
				t.Fatalf("unavailable decision=%v", err)
			}
			refreshed := readPreviewDetails(t, h, id, true)
			if !refreshed.CanApprove || refreshed.PreviewState != turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_READY || refreshed.PreviewHash == first.PreviewHash {
				t.Fatal("explicit refresh did not recover")
			}
			a, err := h.repo.GetApproval(context.Background(), id)
			if err != nil {
				t.Fatal(err)
			}
			if a.Status != "pending" || a.ApprovalToken != "" {
				t.Fatal("refresh authorized the mutation")
			}
		})
	}
}

func TestLiveCheckDistinguishesUnavailableFromObservedChanges(t *testing.T) {
	for _, failure := range []string{"transport", "unreadable", "changed hash", "expected hash mismatch"} {
		t.Run(failure, func(t *testing.T) {
			var failed atomic.Bool
			h, id := previewReadFixture(t, func(w http.ResponseWriter, r *http.Request) {
				p := readyReadSnapshot("before")
				if failed.Load() {
					switch failure {
					case "transport":
						http.Error(w, "unavailable", http.StatusServiceUnavailable)
						return
					case "unreadable":
						p = approvalpreview.Snapshot{State: turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_UNAVAILABLE, ArgumentsJSON: "{}"}
					case "changed hash":
						p = readyReadSnapshot("changed")
					case "expected hash mismatch":
						p = approvalpreview.Snapshot{State: turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_STALE, ArgumentsJSON: "{}"}
					}
				}
				_ = json.NewEncoder(w).Encode(p)
			})
			first := readPreviewDetails(t, h, id, false)
			stored, err := h.repo.GetApprovalPreview(context.Background(), id)
			if err != nil {
				t.Fatal(err)
			}
			failed.Store(true)
			current := readPreviewDetails(t, h, id, false)
			want := turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_STALE
			if failure == "transport" || failure == "unreadable" {
				want = turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_UNAVAILABLE
			}
			if current.PreviewState != want || current.CanApprove || current.PreviewHash != first.PreviewHash {
				t.Errorf("live check=%s, want %s with stable disabled review", current.PreviewState, want)
			}
			if _, err := h.service.ApproveApproval(context.Background(), &turingv1.ApproveApprovalRequest{ApprovalId: id, PreviewHash: first.PreviewHash, ArgsHash: first.ArgsHash}); status.Code(err) != codes.FailedPrecondition {
				t.Fatalf("failed live check approved: %v", err)
			}
			retained, err := h.repo.GetApprovalPreview(context.Background(), id)
			if err != nil {
				t.Fatal(err)
			}
			if retained != stored {
				t.Fatal("ordinary check replaced durable ready snapshot")
			}
			failed.Store(false)
			if recovered := readPreviewDetails(t, h, id, false); !recovered.CanApprove || recovered.PreviewHash != first.PreviewHash {
				t.Fatal("ordinary revalidation of retained READY snapshot failed to recover")
			}
		})
	}
}

func TestConcurrentFirstReadRevalidatesTheOtherReadersSnapshot(t *testing.T) {
	var calls atomic.Int32
	var live atomic.Value
	live.Store("second snapshot")
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	h, id := previewReadFixture(t, func(w http.ResponseWriter, r *http.Request) {
		before := live.Load().(string)
		if calls.Add(1) == 1 {
			before = "first snapshot"
			close(entered)
			<-release
		}
		_ = json.NewEncoder(w).Encode(readyReadSnapshot(before))
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	type result struct {
		details *turingv1.ApprovalDetails
		err     error
	}
	done := make(chan result, 1)
	go func() {
		d, err := h.service.GetApprovalDetails(ctx, &turingv1.GetApprovalDetailsRequest{ApprovalId: id})
		done <- result{d, err}
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	second := readPreviewDetails(t, h, id, false)
	live.Store("changed after second snapshot")
	releaseOnce.Do(func() { close(release) })
	first := <-done
	if first.err != nil {
		t.Fatal(first.err)
	}
	if first.details.PreviewHash != second.PreviewHash || first.details.FilePreview.BeforeText != "second snapshot" ||
		first.details.PreviewState != turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_STALE || first.details.CanApprove {
		t.Fatal("losing first reader skipped revalidation of another snapshot")
	}
	if calls.Load() != 3 {
		t.Fatalf("preparations=%d, want two fresh reads plus one required revalidation", calls.Load())
	}
}
