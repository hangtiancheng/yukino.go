package api

import "context"

type Args struct {
	A int
	B int
}

type Args1 struct {
	A int
	B int
	C int
}

type Reply struct {
	Result int
}

type Arith struct {
}

func (a *Arith) Add(_ context.Context, args *Args) (*Reply, error) {
	return &Reply{Result: args.A + args.B}, nil
}

func (a *Arith) Mul(_ context.Context, args *Args) (*Reply, error) {
	return &Reply{Result: args.A * args.B}, nil
}

type Arith2 struct {
}

func (a *Arith2) Add(_ context.Context, args *Args1) (*Reply, error) {
	return &Reply{Result: args.A + args.B + args.C}, nil
}

func (a *Arith2) Mul(_ context.Context, args *Args1) (*Reply, error) {
	return &Reply{Result: args.A * args.B * args.C}, nil
}
