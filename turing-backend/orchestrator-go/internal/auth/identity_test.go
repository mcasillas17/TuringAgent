package auth

import (
	"context"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const (
	testRuntimeMethod         = "/turing.v1.RuntimeService/ConnectWorker"
	testSessionListMethod     = "/turing.v1.SessionService/ListMessages"
	testApprovalConsumeMethod = "/turing.v1.ApprovalService/ConsumeApproval"
	testApprovalApproveMethod = "/turing.v1.ApprovalService/ApproveApproval"
)

func testIdentities() []ServiceIdentity {
	return []ServiceIdentity{
		NewServiceIdentity("runtime", "runtime-token", testRuntimeMethod, testSessionListMethod, testApprovalConsumeMethod),
		NewServiceIdentity("approval-consumer", "approval-token", testApprovalConsumeMethod),
	}
}

func callUnary(t *testing.T, identities []ServiceIdentity, token string, method string, options InterceptorOptions) (any, error) {
	t.Helper()
	interceptor := UnaryIdentityInterceptor(identities, options)
	ctx := context.Background()
	if token != "" {
		ctx = metadata.NewIncomingContext(ctx, metadata.Pairs("authorization", "Bearer "+token))
	}
	handlerCalled := false
	resp, err := interceptor(ctx, nil, &grpc.UnaryServerInfo{FullMethod: method}, func(context.Context, any) (any, error) {
		handlerCalled = true
		return "ok", nil
	})
	if err == nil && !handlerCalled {
		t.Fatal("interceptor returned success without invoking the handler")
	}
	if err != nil && handlerCalled {
		t.Fatal("interceptor invoked the handler despite returning an error")
	}
	return resp, err
}

func TestUnaryIdentityInterceptorAllowsAllowlistedMethod(t *testing.T) {
	identities := testIdentities()
	if _, err := callUnary(t, identities, "runtime-token", testRuntimeMethod, InterceptorOptions{}); err != nil {
		t.Fatalf("runtime identity calling its own method: err = %v, want nil", err)
	}
	if _, err := callUnary(t, identities, "approval-token", testApprovalConsumeMethod, InterceptorOptions{}); err != nil {
		t.Fatalf("approval-consumer identity calling ConsumeApproval: err = %v, want nil", err)
	}
}

func TestUnaryIdentityInterceptorDeniesWrongServiceMethod(t *testing.T) {
	identities := testIdentities()
	var recorded []Failure
	options := InterceptorOptions{FailureRecorder: func(_ context.Context, failure Failure) error {
		recorded = append(recorded, failure)
		return nil
	}}
	// The approval-consumer identity's token is valid, but ConnectWorker is not
	// in its allowlist: a compromised mcp-files must not be able to claim jobs.
	_, err := callUnary(t, identities, "approval-token", testRuntimeMethod, options)
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("approval-consumer calling ConnectWorker: err = %v, want PermissionDenied", err)
	}
	if len(recorded) != 1 || recorded[0].ActorType != "approval-consumer" {
		t.Fatalf("recorded failures = %+v, want one attributed to approval-consumer", recorded)
	}

	// Symmetrically, the runtime identity must not reach a method reserved for
	// the approval consumer only (SessionService.ListMessages IS in runtime's
	// list, so use a method absent from both to prove denial is per-identity,
	// not just "anything not ConnectWorker").
	_, err = callUnary(t, identities, "runtime-token", testApprovalApproveMethod, options)
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("runtime calling ApproveApproval: err = %v, want PermissionDenied", err)
	}
}

func TestUnaryIdentityInterceptorDeniesUnknownToken(t *testing.T) {
	identities := testIdentities()
	var recorded []Failure
	options := InterceptorOptions{FailureRecorder: func(_ context.Context, failure Failure) error {
		recorded = append(recorded, failure)
		return nil
	}}
	_, err := callUnary(t, identities, "not-a-registered-token", testRuntimeMethod, options)
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("unknown token: err = %v, want Unauthenticated", err)
	}
	if len(recorded) != 1 || recorded[0].ActorType == "runtime" || recorded[0].ActorType == "approval-consumer" {
		t.Fatalf("recorded failures = %+v, want a failure not attributed to a real identity", recorded)
	}
}

func TestUnaryIdentityInterceptorDeniesMalformedOrMissingToken(t *testing.T) {
	identities := testIdentities()
	cases := []struct {
		name  string
		token string
	}{
		{"empty", ""},
		{"garbage", "\x00\xff not-even-utf8 \n\tBearer"},
		{"whitespace", "   "},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := callUnary(t, identities, tc.token, testRuntimeMethod, InterceptorOptions{})
			if status.Code(err) != codes.Unauthenticated {
				t.Fatalf("token %q: err = %v, want Unauthenticated", tc.token, err)
			}
		})
	}

	// No metadata/header at all.
	interceptor := UnaryIdentityInterceptor(identities, InterceptorOptions{})
	_, err := interceptor(context.Background(), nil, &grpc.UnaryServerInfo{FullMethod: testRuntimeMethod}, func(context.Context, any) (any, error) {
		t.Fatal("handler reached with no authorization metadata")
		return nil, nil
	})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("missing metadata: err = %v, want Unauthenticated", err)
	}
}

// A duplicate or empty identity token is a configuration error: it either
// silently merges two identities into one privilege set or can never match
// anything (locking out the identity entirely). NewInternalIdentities catches
// both before the server ever starts.
func TestNewInternalIdentitiesRejectsDuplicateTokens(t *testing.T) {
	_, err := NewInternalIdentities([]ServiceIdentity{
		NewServiceIdentity("runtime", "same-token", testRuntimeMethod),
		NewServiceIdentity("approval-consumer", "same-token", testApprovalConsumeMethod),
	})
	if err == nil {
		t.Fatal("expected an error for identities sharing one token")
	}
}

func TestNewInternalIdentitiesRejectsEmptyToken(t *testing.T) {
	_, err := NewInternalIdentities([]ServiceIdentity{
		NewServiceIdentity("runtime", "", testRuntimeMethod),
	})
	if err == nil {
		t.Fatal("expected an error for an empty identity token")
	}
}

func TestStreamIdentityInterceptorDeniesWrongServiceMethod(t *testing.T) {
	identities := testIdentities()
	var recorded []Failure
	options := InterceptorOptions{FailureRecorder: func(_ context.Context, failure Failure) error {
		recorded = append(recorded, failure)
		return nil
	}}
	interceptor := StreamIdentityInterceptor(identities, options)
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Bearer approval-token"))
	stream := &fakeServerStream{ctx: ctx}
	handlerCalled := false
	err := interceptor(nil, stream, &grpc.StreamServerInfo{FullMethod: testRuntimeMethod}, func(any, grpc.ServerStream) error {
		handlerCalled = true
		return nil
	})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("approval-consumer streaming ConnectWorker: err = %v, want PermissionDenied", err)
	}
	if handlerCalled {
		t.Fatal("stream handler reached despite an unauthorized method")
	}
	if len(recorded) != 1 || recorded[0].ActorType != "approval-consumer" {
		t.Fatalf("recorded failures = %+v, want one attributed to approval-consumer", recorded)
	}
}

func TestStreamIdentityInterceptorAllowsAllowlistedMethod(t *testing.T) {
	identities := testIdentities()
	interceptor := StreamIdentityInterceptor(identities, InterceptorOptions{})
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Bearer runtime-token"))
	stream := &fakeServerStream{ctx: ctx}
	handlerCalled := false
	err := interceptor(nil, stream, &grpc.StreamServerInfo{FullMethod: testRuntimeMethod}, func(any, grpc.ServerStream) error {
		handlerCalled = true
		return nil
	})
	if err != nil {
		t.Fatalf("runtime streaming ConnectWorker: err = %v, want nil", err)
	}
	if !handlerCalled {
		t.Fatal("stream handler was not invoked for an allowlisted method")
	}
}

type fakeServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (f *fakeServerStream) Context() context.Context { return f.ctx }
