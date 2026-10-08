package rpc

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"
)

type fixService struct{}

func (s *fixService) Echo(ctx context.Context, req *streamRequest) (*unaryReply, error) {
	return &unaryReply{Value: req.Count}, nil
}

func (s *fixService) Panic(ctx context.Context, req *streamRequest) (*unaryReply, error) {
	panic("boom")
}

func (s *fixService) BadReturn(ctx context.Context, req *streamRequest) (int, error) {
	return 0, nil
}

// Slow streams must not block unary calls multiplexed on the same connection (H-2).
func (s *fixService) SlowStream(req *streamRequest, stream ServerStream) error {
	for i := 0; i < req.Count; i++ {
		time.Sleep(50 * time.Millisecond)
		if err := stream.Send(&streamChunk{Index: i}); err != nil {
			return err
		}
	}
	return nil
}

func startFixServer(t *testing.T) (string, *Server) {
	t.Helper()
	server := NewServer()
	server.Register("Fix", &fixService{})
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	go server.Serve(lis)
	return lis.Addr().String(), server
}

// H-1: GracefulStop must return even while a client keeps an idle pooled connection open.
func TestGracefulStopWithIdleConnection(t *testing.T) {
	addr, server := startFixServer(t)

	client, err := Dial(addr, WithTimeout(2*time.Second))
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer client.Close()

	var reply unaryReply
	if err := client.Invoke(context.Background(), "Fix", "Echo", &streamRequest{Count: 3}, &reply); err != nil {
		t.Fatalf("Invoke: %v", err)
	}

	done := make(chan struct{})
	go func() {
		server.GracefulStop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("GracefulStop did not return with an idle client connection")
	}
}

// H-2: a slow stream on the shared connection must not starve unary calls.
func TestStreamDoesNotBlockUnary(t *testing.T) {
	addr, server := startFixServer(t)
	defer server.Stop()

	client, err := Dial(addr, WithTimeout(2*time.Second))
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer client.Close()

	stream, err := client.NewStream(context.Background(), "Fix", "SlowStream", &streamRequest{Count: 20})
	if err != nil {
		t.Fatalf("NewStream: %v", err)
	}
	_ = stream

	start := time.Now()
	var reply unaryReply
	if err := client.Invoke(context.Background(), "Fix", "Echo", &streamRequest{Count: 1}, &reply); err != nil {
		t.Fatalf("Invoke while streaming: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("unary call was blocked behind the stream for %s", elapsed)
	}
}

// H-3: handler panics and non-pointer results must surface as errors, not crash the server.
func TestPanicAndBadSignatureDoNotCrashServer(t *testing.T) {
	addr, server := startFixServer(t)
	defer server.Stop()

	client, err := Dial(addr, WithTimeout(2*time.Second))
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer client.Close()

	var reply unaryReply
	err = client.Invoke(context.Background(), "Fix", "Panic", &streamRequest{}, &reply)
	if err == nil || !strings.Contains(err.Error(), "handler panic") {
		t.Fatalf("Panic invoke error = %v, want handler panic", err)
	}

	err = client.Invoke(context.Background(), "Fix", "BadReturn", &streamRequest{}, &reply)
	if err == nil || !strings.Contains(err.Error(), "unsupported method signature") {
		t.Fatalf("BadReturn invoke error = %v, want unsupported signature", err)
	}

	// Server must still be functional after both failures.
	if err := client.Invoke(context.Background(), "Fix", "Echo", &streamRequest{Count: 9}, &reply); err != nil {
		t.Fatalf("Invoke after panic: %v", err)
	}
	if reply.Value != 9 {
		t.Fatalf("reply = %+v", reply)
	}
}

// G-1: the async API must be reachable through the public surface.
func TestInvokeAsyncStatic(t *testing.T) {
	addr, server := startFixServer(t)
	defer server.Stop()

	client, err := Dial(addr, WithTimeout(2*time.Second))
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer client.Close()

	future, err := client.InvokeAsync(context.Background(), "Fix", "Echo", &streamRequest{Count: 5})
	if err != nil {
		t.Fatalf("InvokeAsync: %v", err)
	}
	var reply unaryReply
	if err := future.GetResultWithContext(context.Background(), &reply); err != nil {
		t.Fatalf("GetResultWithContext: %v", err)
	}
	if reply.Value != 5 {
		t.Fatalf("reply = %+v", reply)
	}
}
