// Copyright (c) 2026 hangtiancheng
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package rpc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"testing"
)

type streamChunk struct {
	Content string
	Index   int
}

type streamRequest struct {
	Count int
}

type streamService struct{}

func (s *streamService) Generate(req *streamRequest, stream ServerStream) error {
	for i := 0; i < req.Count; i++ {
		if err := stream.Send(&streamChunk{Content: fmt.Sprintf("chunk-%d", i), Index: i}); err != nil {
			return err
		}
	}
	return nil
}

func (s *streamService) FailMidway(req *streamRequest, stream ServerStream) error {
	for i := 0; i < req.Count; i++ {
		if i == 2 {
			return errors.New("intentional error at chunk 2")
		}
		if err := stream.Send(&streamChunk{Content: fmt.Sprintf("chunk-%d", i), Index: i}); err != nil {
			return err
		}
	}
	return nil
}

type unaryReply struct {
	Value int
}

func (s *streamService) Echo(ctx context.Context, req *streamRequest) (*unaryReply, error) {
	return &unaryReply{Value: req.Count * 2}, nil
}

func startTestServer(t *testing.T) (string, *Server) {
	t.Helper()
	server := NewServer()
	server.Register("Stream", &streamService{})
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	go server.Serve(lis)
	t.Cleanup(server.Stop)
	return lis.Addr().String(), server
}

func TestServerStreaming(t *testing.T) {
	addr, _ := startTestServer(t)

	client, err := Dial(addr)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer client.Close()

	t.Run("basic stream", func(t *testing.T) {
		stream, err := client.NewStream(context.Background(), "Stream", "Generate", &streamRequest{Count: 5})
		if err != nil {
			t.Fatalf("NewStream: %v", err)
		}
		var chunks []streamChunk
		for {
			var chunk streamChunk
			if err := stream.Recv(&chunk); err != nil {
				if err == io.EOF {
					break
				}
				t.Fatalf("Recv: %v", err)
			}
			chunks = append(chunks, chunk)
		}
		if len(chunks) != 5 {
			t.Fatalf("got %d chunks, want 5", len(chunks))
		}
		for i, c := range chunks {
			if c.Index != i || c.Content != fmt.Sprintf("chunk-%d", i) {
				t.Fatalf("chunk[%d] = %+v", i, c)
			}
		}
	})

	t.Run("error mid-stream", func(t *testing.T) {
		stream, err := client.NewStream(context.Background(), "Stream", "FailMidway", &streamRequest{Count: 5})
		if err != nil {
			t.Fatalf("NewStream: %v", err)
		}
		var chunks []streamChunk
		var recvErr error
		for {
			var chunk streamChunk
			if err := stream.Recv(&chunk); err != nil {
				if err == io.EOF {
					break
				}
				recvErr = err
				break
			}
			chunks = append(chunks, chunk)
		}
		if len(chunks) != 2 {
			t.Fatalf("got %d chunks before error, want 2", len(chunks))
		}
		if recvErr == nil {
			t.Fatal("expected error")
		}
		if recvErr.Error() != "intentional error at chunk 2" {
			t.Fatalf("error = %q", recvErr.Error())
		}
	})

	t.Run("unary still works", func(t *testing.T) {
		var reply unaryReply
		if err := client.Invoke(context.Background(), "Stream", "Echo", &streamRequest{Count: 7}, &reply); err != nil {
			t.Fatalf("Invoke: %v", err)
		}
		if reply.Value != 14 {
			t.Fatalf("reply.Value = %d, want 14", reply.Value)
		}
	})

	t.Run("context cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		stream, err := client.NewStream(ctx, "Stream", "Generate", &streamRequest{Count: 100})
		if err != nil {
			t.Fatalf("NewStream: %v", err)
		}
		var chunk streamChunk
		if err := stream.Recv(&chunk); err != nil {
			t.Fatalf("first Recv: %v", err)
		}
		cancel()
		for {
			if err := stream.Recv(&chunk); err != nil {
				break
			}
		}
	})
}
