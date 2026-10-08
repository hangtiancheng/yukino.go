package transport

import (
	"context"
	"sync"
	"time"

	"github.com/hangtiancheng/yukino.go/libs/yukino_rpc/internal/codec"
)

type Future struct {
	done  chan struct{}
	res   []byte
	err   error
	mu    sync.Mutex
	codec codec.Codec

	complete   bool
	onComplete func(error)
}

func NewFuture() *Future {
	return NewFutureWithCodec(nil)
}

func NewFutureWithCodec(cc codec.Codec) *Future {
	if cc == nil {
		var err error
		cc, err = codec.New(codec.JSON)
		if err != nil {
			panic(err)
		}
	}
	return &Future{
		done:  make(chan struct{}),
		codec: cc,
	}
}

func (f *Future) decodeResult(reply any) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.err != nil {
		return f.err
	}
	return f.codec.Unmarshal(f.res, reply)
}

func (f *Future) Done(res []byte, err error) {
	f.mu.Lock()
	if f.complete {
		f.mu.Unlock()
		return
	}
	f.res = res
	f.err = err
	f.complete = true
	onComplete := f.onComplete
	f.mu.Unlock()

	if onComplete != nil {
		onComplete(err)
	}

	close(f.done)
}

func (f *Future) Wait() ([]byte, error) {
	<-f.done
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.res, f.err
}

func (f *Future) OnComplete(fn func(error)) {
	f.mu.Lock()
	if !f.complete {
		f.onComplete = fn
		f.mu.Unlock()
		return
	}
	err := f.err
	f.mu.Unlock()
	if fn != nil {
		fn(err)
	}
}

func (f *Future) WaitWithContext(ctx context.Context) ([]byte, error) {
	select {
	case <-f.done:
		return f.Wait()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (f *Future) DoneChan() <-chan struct{} {
	return f.done
}

func (f *Future) GetResult(reply any) error {
	<-f.done
	return f.decodeResult(reply)
}

func (f *Future) GetResultWithContext(ctx context.Context, reply any) error {
	select {
	case <-f.done:
		return f.GetResult(reply)
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (f *Future) IsDone() bool {
	select {
	case <-f.done:
		return true
	default:
		return false
	}
}

func (f *Future) WaitWithTimeout(timeout time.Duration) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return f.WaitWithContext(ctx)
}
