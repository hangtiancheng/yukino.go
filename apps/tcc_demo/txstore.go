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
