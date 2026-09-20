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

package yukino_cache

import (
	"context"
	"errors"
	"strings"
	"testing"

	pb "github.com/hangtiancheng/yukino.go/yukino_cache/pb"
	"google.golang.org/grpc"
)

func TestClientMethods(t *testing.T) {
	client := &Client{grpcCli: &fakeYukinoCacheClient{
		getFunc: func(ctx context.Context, in *pb.Request, opts ...grpc.CallOption) (*pb.ResponseForGet, error) {
			if in.Group != "group" || in.Key != "key" {
				t.Fatalf("unexpected get request: %+v", in)
			}
			return &pb.ResponseForGet{Value: []byte("value")}, nil
		},
		setFunc: func(ctx context.Context, in *pb.Request, opts ...grpc.CallOption) (*pb.ResponseForGet, error) {
			if string(in.Value) != "value" {
				t.Fatalf("unexpected set value: %q", string(in.Value))
			}
			return &pb.ResponseForGet{Value: in.Value}, nil
		},
		deleteFunc: func(ctx context.Context, in *pb.Request, opts ...grpc.CallOption) (*pb.ResponseForDelete, error) {
			return &pb.ResponseForDelete{Value: true}, nil
		},
	}}

	value, err := client.Get("group", "key")
	if err != nil || string(value) != "value" {
		t.Fatalf("Get returned %q, %v", string(value), err)
	}
	if err := client.Set(context.Background(), "group", "key", []byte("value")); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}
	deleted, err := client.Delete("group", "key")
	if err != nil || !deleted {
		t.Fatalf("Delete returned %v, %v", deleted, err)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
}

func TestClientMethodErrors(t *testing.T) {
	wantErr := errors.New("rpc failed")
	client := &Client{grpcCli: &fakeYukinoCacheClient{
		getFunc: func(ctx context.Context, in *pb.Request, opts ...grpc.CallOption) (*pb.ResponseForGet, error) {
			return nil, wantErr
		},
		setFunc: func(ctx context.Context, in *pb.Request, opts ...grpc.CallOption) (*pb.ResponseForGet, error) {
			return nil, wantErr
		},
		deleteFunc: func(ctx context.Context, in *pb.Request, opts ...grpc.CallOption) (*pb.ResponseForDelete, error) {
			return nil, wantErr
		},
	}}

	if _, err := client.Get("group", "key"); err == nil || !strings.Contains(err.Error(), "failed to get value") {
		t.Fatalf("unexpected Get error: %v", err)
	}
	if err := client.Set(context.Background(), "group", "key", []byte("value")); err == nil || !strings.Contains(err.Error(), "failed to set value") {
		t.Fatalf("unexpected Set error: %v", err)
	}
	if _, err := client.Delete("group", "key"); err == nil || !strings.Contains(err.Error(), "failed to delete value") {
		t.Fatalf("unexpected Delete error: %v", err)
	}
}

type fakeYukinoCacheClient struct {
	getFunc    func(ctx context.Context, in *pb.Request, opts ...grpc.CallOption) (*pb.ResponseForGet, error)
	setFunc    func(ctx context.Context, in *pb.Request, opts ...grpc.CallOption) (*pb.ResponseForGet, error)
	deleteFunc func(ctx context.Context, in *pb.Request, opts ...grpc.CallOption) (*pb.ResponseForDelete, error)
}

func (c *fakeYukinoCacheClient) Get(ctx context.Context, in *pb.Request, opts ...grpc.CallOption) (*pb.ResponseForGet, error) {
	return c.getFunc(ctx, in, opts...)
}

func (c *fakeYukinoCacheClient) Set(ctx context.Context, in *pb.Request, opts ...grpc.CallOption) (*pb.ResponseForGet, error) {
	return c.setFunc(ctx, in, opts...)
}

func (c *fakeYukinoCacheClient) Delete(ctx context.Context, in *pb.Request, opts ...grpc.CallOption) (*pb.ResponseForDelete, error) {
	return c.deleteFunc(ctx, in, opts...)
}
