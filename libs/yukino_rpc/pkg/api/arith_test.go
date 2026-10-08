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
