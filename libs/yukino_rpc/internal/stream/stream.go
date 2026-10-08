package stream

import "context"

type ServerStream interface {
	Send(msg any) error
	Context() context.Context
}

type ClientStream interface {
	Recv(msg any) error
	Context() context.Context
}
