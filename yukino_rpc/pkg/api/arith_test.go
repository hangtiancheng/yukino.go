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

package api

import (
	"context"
	"testing"
)

func TestArith(t *testing.T) {
	var srv Arith
	reply, err := srv.Add(context.Background(), &Args{A: 2, B: 3})
	if err != nil {
		t.Fatalf("Add returned error: %v", err)
	}
	if reply.Result != 5 {
		t.Fatalf("Add result = %d, want 5", reply.Result)
	}
	reply, err = srv.Mul(context.Background(), &Args{A: 4, B: 5})
	if err != nil {
		t.Fatalf("Mul returned error: %v", err)
	}
	if reply.Result != 20 {
		t.Fatalf("Mul result = %d, want 20", reply.Result)
	}
}

func TestArith2(t *testing.T) {
	var srv Arith2
	reply, err := srv.Add(context.Background(), &Args1{A: 1, B: 2, C: 3})
	if err != nil {
		t.Fatalf("Add returned error: %v", err)
	}
	if reply.Result != 6 {
		t.Fatalf("Add result = %d, want 6", reply.Result)
	}
	reply, err = srv.Mul(context.Background(), &Args1{A: 2, B: 3, C: 4})
	if err != nil {
		t.Fatalf("Mul returned error: %v", err)
	}
	if reply.Result != 24 {
		t.Fatalf("Mul result = %d, want 24", reply.Result)
	}
}
