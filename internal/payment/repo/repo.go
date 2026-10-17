package repo

import (
	"context"
	"database/sql"
	"finote/internal/payment/model"
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
