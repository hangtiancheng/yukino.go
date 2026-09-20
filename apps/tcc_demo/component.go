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

import "context"

// TCC request parameters
type TCCReq struct {
	// Globally unique transaction ID
	ComponentID string         `json:"componentID"`
	TXID        string         `json:"txID"`
	Data        map[string]any `json:"data"`
}

// TCC response result
type TCCResp struct {
	ComponentID string `json:"componentID"`
	ACK         bool   `json:"ack"`
	TXID        string `json:"txID"`
}

// TCC component interface
type TCCComponent interface {
	// Returns the unique component ID
	ID() string
	// Executes the first-phase try operation
	Try(ctx context.Context, req *TCCReq) (*TCCResp, error)
	// Executes the second-phase confirm operation
	Confirm(ctx context.Context, txID string) (*TCCResp, error)
	// Executes the second-phase cancel operation
	Cancel(ctx context.Context, txID string) (*TCCResp, error)
}
