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
