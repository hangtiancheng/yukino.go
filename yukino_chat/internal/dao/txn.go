package dao

import (
	"context"
	"strings"

	"github.com/hangtiancheng/yukino.go/yukino_orm"
)

// WithTransaction runs fn inside a MongoDB transaction. Standalone mongod
// deployments do not support transactions; in that case the callback is
// re-run without one so multi-document writes still execute sequentially.
func WithTransaction(ctx context.Context, fn func(ctx context.Context, e *yukino_orm.Engine) error) error {
	err := Engine.Transaction(ctx, func(sc context.Context, tx *yukino_orm.Engine) error {
		return fn(sc, tx)
	})
	if err != nil && transactionsUnsupported(err) {
		return fn(ctx, Engine)
	}
	return err
}

func transactionsUnsupported(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "Transaction numbers are only allowed") ||
		strings.Contains(msg, "transactions are not supported") ||
		strings.Contains(msg, "IllegalOperation")
}
