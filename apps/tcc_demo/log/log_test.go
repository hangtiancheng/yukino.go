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

package log

import (
	"context"
	"testing"
	"time"
)

func Test_customer_logger(t *testing.T) {
	logger := NewSugarLogger(NewOptions(
		WithFileName("tcc_demo.log"),
		WithLogLevel("info"),
	))
	logger.Info("test customer logger running...")
}

func Test_default_logger(t *testing.T) {
	now := time.Now()
	Debugf("debug... now: %v", now)
	Infof("info... now: %v", now)
	Warnf("warn... now: %v", now)
	Errorf("error... now: %v", now)
	Fatalf("fatal... now: %v", now)

	ctx := context.Background()
	DebugContext(ctx, "debug...")
	DebugContextf(ctx, "debug... now: %v", now)
	InfoContext(ctx, "info...")
	InfoContextf(ctx, "info... now: %v", now)
	WarnContext(ctx, "warn...")
	WarnContextf(ctx, "warn... now: %v", now)
	ErrorContext(ctx, "error...")
	ErrorContextf(ctx, "error... now: %v", now)
}
