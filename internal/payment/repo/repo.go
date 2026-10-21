package repo

import (
	"context"
	"database/sql"
	"errors"
	"finote/internal/payment/model"
	"time"
)

type Repo struct {
	db    *sql.DB
	query DBTX
	owner string
}

type DBTX interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func New(db *sql.DB) *Repo {
	return &Repo{db: db, query: db}
}

// NewOwnedTx leaves commit to the caller, keeping expenses and receipts atomic.
func NewOwnedTx(tx *sql.Tx, owner string) (*Repo, error) {
	if tx == nil || owner == "" {
		return nil, errors.New("transaction and owner are required")
	}
	return &Repo{query: tx, owner: owner}, nil
}

func (r *Repo) CreatePayment(ctx context.Context, body model.Payment) (model.Payment, error) {
	if r.owner != "" {
		body.UserID = r.owner
	}
	const query = `INSERT INTO payments (id, user_id, category, amount, comment, spent_at, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING id, user_id, category, amount, comment, spent_at, created_at, updated_at`

	var payment model.Payment
	err := r.query.QueryRowContext(ctx, query,
		body.ID,
		body.UserID,
		body.Category,
		body.Amount,
		body.Comment,
		body.SpentAt,
		body.CreatedAt,
		body.UpdatedAt,
	).Scan(
		&payment.ID,
		&payment.UserID,
		&payment.Category,
		&payment.Amount,
		&payment.Comment,
		&payment.SpentAt,
		&payment.CreatedAt,
		&payment.UpdatedAt,
	)
	return payment, err
}

func (r *Repo) GetPayment(ctx context.Context, id string) (model.Payment, error) {
	const query = `
		SELECT id, user_id, category, amount, comment, spent_at, created_at, updated_at
		FROM payments
		WHERE id = $1 AND ($2 = '' OR user_id = $2)
	`

	var payment model.Payment
	err := r.query.QueryRowContext(ctx, query, id, r.owner).Scan(
		&payment.ID,
		&payment.UserID,
		&payment.Category,
		&payment.Amount,
		&payment.Comment,
		&payment.SpentAt,
		&payment.CreatedAt,
		&payment.UpdatedAt,
	)
	return payment, err
}

func (r *Repo) UpdatePayment(ctx context.Context, id string, body model.UpdatePayment, updatedAt time.Time) (model.Payment, error) {
	const query = `
		UPDATE payments
		SET category = COALESCE($2, category),
		    amount = COALESCE($3, amount),
		    comment = COALESCE($4, comment),
		    spent_at = COALESCE($5, spent_at),
		    updated_at = $6
		WHERE id = $1 AND ($7 = '' OR user_id = $7)
		RETURNING id, user_id, category, amount, comment,
		          spent_at, created_at, updated_at
	`

	var payment model.Payment
	err := r.query.QueryRowContext(
		ctx,
		query,
		id,
		body.Category,
		body.Amount,
		body.Comment,
		body.SpentAt,
		updatedAt,
		r.owner,
	).Scan(
		&payment.ID,
		&payment.UserID,
		&payment.Category,
		&payment.Amount,
		&payment.Comment,
		&payment.SpentAt,
		&payment.CreatedAt,
		&payment.UpdatedAt,
	)

	return payment, err
}

func (r *Repo) DeletePayment(ctx context.Context, id string) error {
	const query = `DELETE FROM payments WHERE id = $1 AND ($2 = '' OR user_id = $2)`
	result, err := r.query.ExecContext(ctx, query, id, r.owner)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}
