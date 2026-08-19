package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TransactionFunc is invoked exactly once with a transaction-bound query
// handle. It must not retain the handle after returning.
type TransactionFunc func(context.Context, DBTX) error

// Transactor owns transaction completion. Callers cannot commit or roll back
// because TransactionFunc receives only DBTX.
type Transactor struct {
	pool *pgxpool.Pool
}

// NewTransactor wraps a pgx connection pool.
func NewTransactor(pool *pgxpool.Pool) (*Transactor, error) {
	if pool == nil {
		return nil, errors.New("postgres transactor requires a pool")
	}
	return &Transactor{pool: pool}, nil
}

// WithinTransaction executes fn in a PostgreSQL transaction. A callback error
// is returned after rollback; a successful callback is committed. Commit and
// rollback failures retain enough context for internal diagnostics.
func (t *Transactor) WithinTransaction(ctx context.Context, options pgx.TxOptions, fn TransactionFunc) error {
	if fn == nil {
		return errors.New("postgres transaction requires a callback")
	}
	tx, err := t.pool.BeginTx(ctx, options)
	if err != nil {
		return fmt.Errorf("begin postgres transaction: %w", err)
	}

	if err := fn(ctx, tx); err != nil {
		if rollbackErr := tx.Rollback(context.WithoutCancel(ctx)); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			return errors.Join(err, fmt.Errorf("rollback postgres transaction: %w", rollbackErr))
		}
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit postgres transaction: %w", err)
	}
	return nil
}
