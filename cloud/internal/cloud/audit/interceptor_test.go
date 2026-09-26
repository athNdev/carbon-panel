package audit

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"connectrpc.com/connect"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/principal"
	cloudv1connect "github.com/athNdev/carbon-panel/pkg/proto/cloud/v1/cloudv1connect"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"
)

// injectInterceptor puts a fixed principal into the handler context so tests
// can exercise the server side of the audit interceptor.
type injectInterceptor struct{ p principal.Principal }

func (injectInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

func (x injectInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		return next(principal.WithPrincipal(ctx, x.p), req)
	}
}

func (x injectInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		return next(principal.WithPrincipal(ctx, x.p), conn)
	}
}

var testPrincipal = principal.Principal{
	Kind:   principal.KindSession,
	UserID: "user-1",
	OrgID:  "org-a",
	Role:   "owner",
}

func serveUnary(t *testing.T, procedure string, fn func(context.Context, *connect.Request[emptypb.Empty]) (*connect.Response[emptypb.Empty], error), interceptors ...connect.Interceptor) string {
	t.Helper()
	mux := http.NewServeMux()
	mux.Handle(procedure, connect.NewUnaryHandler(procedure, fn, connect.WithInterceptors(interceptors...)))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL
}

func callUnary(t *testing.T, url, procedure string) error {
	t.Helper()
	client := connect.NewClient[emptypb.Empty, emptypb.Empty](http.DefaultClient, url+procedure)
	_, err := client.CallUnary(context.Background(), connect.NewRequest(&emptypb.Empty{}))
	return err
}

func okHandler(context.Context, *connect.Request[emptypb.Empty]) (*connect.Response[emptypb.Empty], error) {
	return connect.NewResponse(&emptypb.Empty{}), nil
}

func TestInterceptorWritesMutatingUnary(t *testing.T) {
	t.Parallel()
	st := NewGormStore(openTestStore(t))
	proc := cloudv1connect.NodeServiceDeleteNodeProcedure
	url := serveUnary(t, proc, okHandler,
		injectInterceptor{p: testPrincipal}, Interceptor(st, Options{}))
	require.NoError(t, callUnary(t, url, proc))

	events, _, err := st.List(principal.WithPrincipal(context.Background(), testPrincipal), Filter{})
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, "nodes.delete", events[0].Action)
	require.Equal(t, ResultOK, events[0].Result)
	require.Equal(t, "user-1", events[0].ActorUserID)
}

func TestInterceptorSkipsReadOnly(t *testing.T) {
	t.Parallel()
	st := NewGormStore(openTestStore(t))
	proc := cloudv1connect.NodeServiceListNodesProcedure
	url := serveUnary(t, proc, okHandler,
		injectInterceptor{p: testPrincipal}, Interceptor(st, Options{}))
	require.NoError(t, callUnary(t, url, proc))

	events, _, err := st.List(principal.WithPrincipal(context.Background(), testPrincipal), Filter{})
	require.NoError(t, err)
	require.Empty(t, events)
}

func TestInterceptorFailureRecordsErrorClass(t *testing.T) {
	t.Parallel()
	const leak = "boom-internal-detail-xyz"
	st := NewGormStore(openTestStore(t))
	proc := cloudv1connect.WorkloadServiceStartWorkloadProcedure
	bad := func(context.Context, *connect.Request[emptypb.Empty]) (*connect.Response[emptypb.Empty], error) {
		return nil, connect.NewError(connect.CodeInternal, errors.New(leak))
	}
	url := serveUnary(t, proc, bad,
		injectInterceptor{p: testPrincipal}, Interceptor(st, Options{}))
	err := callUnary(t, url, proc)
	require.Error(t, err)
	require.Equal(t, connect.CodeInternal, connect.CodeOf(err), "RPC error must pass through")

	events, _, lerr := st.List(principal.WithPrincipal(context.Background(), testPrincipal), Filter{})
	require.NoError(t, lerr)
	require.Len(t, events, 1)
	require.Equal(t, ResultError, events[0].Result)
	require.Equal(t, "internal", events[0].Detail["error_code"])
	require.NotContains(t, events[0].Detail["error_code"], leak)
}

func TestInterceptorDenialResult(t *testing.T) {
	t.Parallel()
	st := NewGormStore(openTestStore(t))
	proc := cloudv1connect.NodeServiceDeleteNodeProcedure
	deny := func(context.Context, *connect.Request[emptypb.Empty]) (*connect.Response[emptypb.Empty], error) {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("nope"))
	}
	url := serveUnary(t, proc, deny,
		injectInterceptor{p: testPrincipal}, Interceptor(st, Options{}))
	require.Error(t, callUnary(t, url, proc))

	events, _, lerr := st.List(principal.WithPrincipal(context.Background(), testPrincipal), Filter{})
	require.NoError(t, lerr)
	require.Len(t, events, 1)
	require.Equal(t, ResultDenied, events[0].Result)
}

type failingStore struct{ err error }

func (f failingStore) Append(context.Context, Event) error { return f.err }
func (f failingStore) List(context.Context, Filter) ([]Event, string, error) {
	return nil, "", f.err
}
func (f failingStore) Get(context.Context, string) (Event, error) { return Event{}, f.err }

func TestInterceptorAuditFailureDoesNotFailRPC(t *testing.T) {
	t.Parallel()
	var gotErr error
	var gotEvt Event
	opts := Options{OnError: func(_ context.Context, e Event, err error) {
		gotEvt, gotErr = e, err
	}}
	in := Interceptor(failingStore{err: errors.New("db down")}, opts)
	proc := cloudv1connect.NodeServiceDeleteNodeProcedure
	url := serveUnary(t, proc, okHandler, injectInterceptor{p: testPrincipal}, in)
	require.NoError(t, callUnary(t, url, proc), "audit write failure must not fail the RPC")
	require.ErrorContains(t, gotErr, "db down")
	require.Equal(t, "nodes.delete", gotEvt.Action)
	require.EqualValues(t, 1, Failures(in))
}

func TestInterceptorNoPrincipalRefusesWithoutSilence(t *testing.T) {
	t.Parallel()
	st := NewGormStore(openTestStore(t))
	var gotErr error
	in := Interceptor(st, Options{OnError: func(_ context.Context, _ Event, err error) {
		gotErr = err
	}})
	proc := cloudv1connect.NodeServiceDeleteNodeProcedure
	url := serveUnary(t, proc, okHandler, in) // no principal injected
	require.NoError(t, callUnary(t, url, proc))
	require.ErrorIs(t, gotErr, ErrNoActor)
	require.EqualValues(t, 1, Failures(in))
}

// fakeStreamConn exercises the streaming-handler path without a network.
type fakeStreamConn struct{ procedure string }

func (f fakeStreamConn) Spec() connect.Spec { return connect.Spec{Procedure: f.procedure} }
func (f fakeStreamConn) Peer() connect.Peer { return connect.Peer{Addr: "10.0.0.2:1234"} }
func (f fakeStreamConn) Receive(any) error  { return nil }
func (f fakeStreamConn) RequestHeader() http.Header {
	return http.Header{"User-Agent": []string{"test-agent"}}
}
func (f fakeStreamConn) Send(any) error              { return nil }
func (f fakeStreamConn) ResponseHeader() http.Header { return http.Header{} }
func (f fakeStreamConn) ResponseTrailer() http.Header {
	return http.Header{}
}

func TestInterceptorStreamingHandler(t *testing.T) {
	t.Parallel()
	st := NewGormStore(openTestStore(t))
	in := Interceptor(st, Options{})
	ctx := principal.WithPrincipal(context.Background(), testPrincipal)

	next := func(context.Context, connect.StreamingHandlerConn) error { return nil }
	// Mutating procedure on a stream writes exactly one event.
	require.NoError(t, in.WrapStreamingHandler(next)(ctx,
		fakeStreamConn{procedure: cloudv1connect.WorkloadServiceSendWorkloadCommandProcedure}))
	// Read-only stream writes nothing.
	require.NoError(t, in.WrapStreamingHandler(next)(ctx,
		fakeStreamConn{procedure: cloudv1connect.WorkloadServiceStreamWorkloadLogsProcedure}))

	events, _, err := st.List(ctx, Filter{})
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, "workloads.exec", events[0].Action)
	require.Equal(t, "10.0.0.2:1234", events[0].IP)
}
