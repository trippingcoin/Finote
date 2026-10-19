package repo

import (
	"context"
	"database/sql"
	"finote/internal/payment/model"
	"time"
)

type Repo struct {
	db *sql.DB
}

func New(db *sql.DB) *Repo {
	return &Repo{db: db}
}

func (r *Repo) CreatePayment(ctx context.Context, body model.Payment) (model.Payment, error) {
	const query = `INSERT INTO payments (id, user_id, category, amount, comment, spent_at, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING id, user_id, category, amount, comment, spent_at, created_at, updated_at`

	var payment model.Payment
	err := r.db.QueryRowContext(ctx, query,
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
		WHERE id = $1
	`

	var payment model.Payment
	err := r.db.QueryRowContext(ctx, query, id).Scan(
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
		WHERE id = $1
		RETURNING id, user_id, category, amount, comment,
		          spent_at, created_at, updated_at
	`

	var payment model.Payment
	err := r.db.QueryRowContext(
		ctx,
		query,
		id,
		body.Category,
		body.Amount,
		body.Comment,
		body.SpentAt,
		updatedAt,
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
	const query = `DELETE FROM payments WHERE id = $1`
	_, err := r.db.ExecContext(ctx, query, id)
	return err
}
