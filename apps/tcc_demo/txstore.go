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

package tcc_demo

import (
	"context"
	"time"
)

// Transaction log storage module
type TXStore interface {
	// Creates a transaction detail record
	CreateTX(ctx context.Context, components ...TCCComponent) (txID string, err error)
	// Updates transaction progress: updates each component's try response result
	TXUpdate(ctx context.Context, txID string, componentID string, accept bool) error
	// Submits the final transaction status, indicating success or failure
	TXSubmit(ctx context.Context, txID string, success bool) error
	// Retrieves all incomplete transactions
	GetHangingTXs(ctx context.Context) ([]*Transaction, error)
	// Retrieves a specific transaction by ID
	GetTX(ctx context.Context, txID string) (*Transaction, error)
	// Locks the entire TXStore module (requires a distributed lock)
	Lock(ctx context.Context, expireDuration time.Duration) error
	// Unlocks the TXStore module
	Unlock(ctx context.Context) error
}
