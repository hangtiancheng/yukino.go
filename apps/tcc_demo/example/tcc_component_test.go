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

package example

import (
	"context"
	"errors"
	"testing"

	"github.com/hangtiancheng/yukino.go/apps/redis_lock"
	"github.com/hangtiancheng/yukino.go/apps/tcc_demo"
	"github.com/hangtiancheng/yukino.go/apps/tcc_demo/example/mocks"
	"github.com/hangtiancheng/yukino.go/apps/tcc_demo/example/pkg"
	"go.uber.org/mock/gomock"
)

func Test_MockComponent_Try(t *testing.T) {
	lockErr := "lockErr"
	lockErrCtxKey := &lockErr

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockClient := mocks.NewMockRedisClient(ctrl)
	mockLock := mocks.NewMockLock(ctrl)

	mockLock.EXPECT().Lock(gomock.Any()).DoAndReturn(func(ctx context.Context) error {
		if v, _ := ctx.Value(lockErrCtxKey).(bool); v {
			return errors.New("lock err")
		}
		return nil
	}).AnyTimes()
	mockLock.EXPECT().Unlock(gomock.Any()).Return(nil).AnyTimes()

	mockClient.EXPECT().Get(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, key string) (string, error) {
		switch key {
		case pkg.BuildTXKey("id", "err"):
			return "", errors.New("getErr")
		case pkg.BuildTXKey("id", "repeat"):
			return TXConfirmed.String(), nil
		case pkg.BuildTXKey("id", "cancel"):
			return TXCanceled.String(), nil
		default:
			return "", nil
		}
	}).AnyTimes()

	mockClient.EXPECT().Set(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, key string, value string) (int64, error) {
		if value == "setTXToBizErr" {
			return -1, errors.New("setTXToBizErr")
		}
		if key == pkg.BuildTXKey("id", "setTxStatusErr") {
			return -1, errors.New("setTxStatusErr")
		}
		return 1, nil
	}).AnyTimes()

	mockClient.EXPECT().SetNX(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, key string, value string) (int64, error) {
		switch key {
		case pkg.BuildDataKey("id", "tx", "frozeBizErr"):
			return -1, errors.New("frozeBizErr")
		case pkg.BuildDataKey("id", "tx", "frozeBizFail"):
			return 0, nil
		default:
			return 1, nil
		}
	}).AnyTimes()

	mockComponent := &MockComponent{
		id:     "id",
		client: mockClient,
		lockFactory: func(key string, opts ...redis_lock.LockOption) Lock {
			return mockLock
		},
	}

	if mockComponent.ID() != "id" {
		t.Errorf("expected id, got %s", mockComponent.ID())
	}

	ctx := context.Background()

	tests := []struct {
		name      string
		ctx       context.Context
		req       *tcc_demo.TCCReq
		expectErr bool
		ack       bool
	}{
		{
			name:      "lockErr",
			ctx:       context.WithValue(ctx, lockErrCtxKey, true),
			req:       &tcc_demo.TCCReq{},
			expectErr: true,
		},
		{
			name: "getTXKeyErr",
			ctx:  ctx,
			req: &tcc_demo.TCCReq{
				TXID: "err",
			},
			expectErr: true,
		},
		{
			name: "getTXKeyRepeat",
			ctx:  ctx,
			req: &tcc_demo.TCCReq{
				TXID: "repeat",
			},
			ack: true,
		},
		{
			name: "getTXKeyCancel",
			ctx:  ctx,
			req: &tcc_demo.TCCReq{
				TXID: "cancel",
			},
		},
		{
			name: "setTXToBizErr",
			ctx:  ctx,
			req: &tcc_demo.TCCReq{
				TXID: "tx",
				Data: map[string]any{
					"biz_id": "setTXToBizErr",
				},
			},
			expectErr: true,
		},
		{
			name: "frozeBizErr",
			ctx:  ctx,
			req: &tcc_demo.TCCReq{
				TXID: "tx",
				Data: map[string]any{
					"biz_id": "frozeBizErr",
				},
			},
			expectErr: true,
		},
		{
			name: "frozeBizFail",
			ctx:  ctx,
			req: &tcc_demo.TCCReq{
				TXID: "tx",
				Data: map[string]any{
					"biz_id": "frozeBizFail",
				},
			},
		},
		{
			name: "setTxStatusErr",
			ctx:  ctx,
			req: &tcc_demo.TCCReq{
				TXID: "setTxStatusErr",
			},
			expectErr: true,
		},
		{
			name: "success",
			ctx:  ctx,
			req: &tcc_demo.TCCReq{
				TXID: "success",
			},
			ack: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := mockComponent.Try(tt.ctx, tt.req)
			if tt.expectErr != (err != nil) {
				t.Errorf("expected err=%v, got %v", tt.expectErr, err)
				return
			}
			if err != nil {
				return
			}
			if tt.ack != resp.ACK {
				t.Errorf("expected ack=%v, got %v", tt.ack, resp.ACK)
			}
		})
	}
}

func Test_MockComponent_Confirm(t *testing.T) {
	lockErr := "lockErr"
	lockErrCtxKey := &lockErr

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockClient := mocks.NewMockRedisClient(ctrl)
	mockLock := mocks.NewMockLock(ctrl)

	mockLock.EXPECT().Lock(gomock.Any()).DoAndReturn(func(ctx context.Context) error {
		if v, _ := ctx.Value(lockErrCtxKey).(bool); v {
			return errors.New("lock err")
		}
		return nil
	}).AnyTimes()
	mockLock.EXPECT().Unlock(gomock.Any()).Return(nil).AnyTimes()

	mockClient.EXPECT().Get(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, key string) (string, error) {
		switch key {
		case pkg.BuildTXKey("id", "err"):
			return "", errors.New("getErr")
		case pkg.BuildTXKey("id", "repeat"):
			return TXConfirmed.String(), nil
		case pkg.BuildTXKey("id", "cancel"):
			return TXCanceled.String(), nil
		case pkg.BuildTXDetailKey("id", "txToBizErr"):
			return "", errors.New("txToBizErr")
		case pkg.BuildDataKey("id", "getBizErr", "tried"):
			return "", errors.New("getBizErr")
		case pkg.BuildDataKey("id", "getBizUnfrozen", "tried"):
			return "", nil
		case pkg.BuildDataKey("id", "setBizResErr", "tried"):
			return DataFrozen.String(), nil
		case pkg.BuildDataKey("id", "success", "tried"):
			return DataFrozen.String(), nil
		default:
			return TXTried.String(), nil
		}
	}).AnyTimes()

	mockClient.EXPECT().Set(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, key string, value string) (int64, error) {
		if key == pkg.BuildDataKey("id", "setBizResErr", "tried") {
			return -1, errors.New("setBizResErr")
		}
		return 1, nil
	}).AnyTimes()

	mockComponent := &MockComponent{
		id:     "id",
		client: mockClient,
		lockFactory: func(key string, opts ...redis_lock.LockOption) Lock {
			return mockLock
		},
	}

	if mockComponent.ID() != "id" {
		t.Errorf("expected id, got %s", mockComponent.ID())
	}

	ctx := context.Background()

	tests := []struct {
		name      string
		ctx       context.Context
		txid      string
		expectErr bool
		ack       bool
	}{
		{
			name:      "lockErr",
			ctx:       context.WithValue(ctx, lockErrCtxKey, true),
			expectErr: true,
		},
		{
			name:      "getTXKeyErr",
			ctx:       ctx,
			txid:      "err",
			expectErr: true,
		},
		{
			name: "getTXKeyRepeat",
			ctx:  ctx,
			txid: "repeat",
			ack:  true,
		},
		{
			name: "getTXKeyCancel",
			ctx:  ctx,
			txid: "cancel",
		},
		{
			name:      "txToBizErr",
			ctx:       ctx,
			txid:      "txToBizErr",
			expectErr: true,
		},
		{
			name:      "getBizErr",
			ctx:       ctx,
			txid:      "getBizErr",
			expectErr: true,
		},
		{
			name: "getBizUnfrozen",
			ctx:  ctx,
			txid: "getBizUnfrozen",
		},
		{
			name:      "setBizResErr",
			ctx:       ctx,
			txid:      "setBizResErr",
			expectErr: true,
		},
		{
			name: "success",
			ctx:  ctx,
			txid: "success",
			ack:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := mockComponent.Confirm(tt.ctx, tt.txid)
			if tt.expectErr != (err != nil) {
				t.Errorf("expected err=%v, got %v", tt.expectErr, err)
				return
			}
			if err != nil {
				return
			}
			if tt.ack != resp.ACK {
				t.Errorf("expected ack=%v, got %v", tt.ack, resp.ACK)
			}
		})
	}
}

func Test_MockComponent_Cancel(t *testing.T) {
	lockErr := "lockErr"
	lockErrCtxKey := &lockErr

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockClient := mocks.NewMockRedisClient(ctrl)
	mockLock := mocks.NewMockLock(ctrl)

	mockLock.EXPECT().Lock(gomock.Any()).DoAndReturn(func(ctx context.Context) error {
		if v, _ := ctx.Value(lockErrCtxKey).(bool); v {
			return errors.New("lock err")
		}
		return nil
	}).AnyTimes()
	mockLock.EXPECT().Unlock(gomock.Any()).Return(nil).AnyTimes()

	mockClient.EXPECT().Get(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, key string) (string, error) {
		switch key {
		case pkg.BuildTXKey("id", "err"):
			return "", errors.New("getErr")
		case pkg.BuildTXKey("id", "invalidTXStatus"):
			return TXConfirmed.String(), nil
		case pkg.BuildTXDetailKey("id", "getBizErr"):
			return "", errors.New("getBizErr")
		case pkg.BuildTXDetailKey("id", "deleteBizFrozeErr"):
			return "deleteBizFrozeErr", nil
		default:
			return TXTried.String(), nil
		}
	}).AnyTimes()

	mockClient.EXPECT().Del(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, key string) error {
		if key == pkg.BuildDataKey("id", "deleteBizFrozeErr", "deleteBizFrozeErr") {
			return errors.New("deleteBizFrozeErr")
		}
		return nil
	}).AnyTimes()

	mockClient.EXPECT().Set(gomock.Any(), gomock.Any(), gomock.Any()).Return(int64(1), nil).AnyTimes()

	mockComponent := &MockComponent{
		id:     "id",
		client: mockClient,
		lockFactory: func(key string, opts ...redis_lock.LockOption) Lock {
			return mockLock
		},
	}

	if mockComponent.ID() != "id" {
		t.Errorf("expected id, got %s", mockComponent.ID())
	}

	ctx := context.Background()

	tests := []struct {
		name      string
		ctx       context.Context
		txid      string
		expectErr bool
		ack       bool
	}{
		{
			name:      "lockErr",
			ctx:       context.WithValue(ctx, lockErrCtxKey, true),
			expectErr: true,
		},
		{
			name:      "getTXKeyErr",
			ctx:       ctx,
			txid:      "err",
			expectErr: true,
		},
		{
			name:      "invalidTXStatus",
			ctx:       ctx,
			txid:      "invalidTXStatus",
			expectErr: true,
		},
		{
			name:      "getBizErr",
			ctx:       ctx,
			txid:      "getBizErr",
			expectErr: true,
		},
		{
			name:      "deleteBizFrozeErr",
			ctx:       ctx,
			txid:      "deleteBizFrozeErr",
			expectErr: true,
		},
		{
			name: "success",
			ctx:  ctx,
			txid: "success",
			ack:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := mockComponent.Cancel(tt.ctx, tt.txid)
			if tt.expectErr != (err != nil) {
				t.Errorf("expected err=%v, got %v", tt.expectErr, err)
				return
			}
			if err != nil {
				return
			}
			if tt.ack != resp.ACK {
				t.Errorf("expected ack=%v, got %v", tt.ack, resp.ACK)
			}
		})
	}
}
