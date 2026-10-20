package repo

import (
	"context"
	"database/sql"
	"finote/internal/payment/model"
	"fmt"
	"strings"
)

func (r *Repo) ListPayments(ctx context.Context, f model.Filter) (model.PaymentList, error) {
	result := model.PaymentList{Items: []model.Payment{}, Limit: f.Limit, Offset: f.Offset}
	parts := []string{"TRUE"}
	args := []any{}
	add := func(expr string, value any) {
		args = append(args, value)
		parts = append(parts, fmt.Sprintf(expr, len(args)))
	}
	if f.UserID != "" {
		add("user_id=$%d", f.UserID)
	}
	if f.Category != "" {
		add("category=$%d", f.Category)
	}
	if f.From != nil {
		add("spent_at >= $%d", *f.From)
	}
	if f.To != nil {
		add("spent_at < $%d", *f.To)
	}
	where := strings.Join(parts, " AND ")
	// Totals and this page must describe the same snapshot, including concurrent bot writes.
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	err = tx.QueryRowContext(ctx, "SELECT COUNT(*),COALESCE(SUM(amount),0)::text,COUNT(DISTINCT user_id) FROM payments WHERE "+where, args...).Scan(&result.Total, &result.Amount, &result.Members)
	if err != nil {
		return result, err
	}
	args = append(args, f.Limit, f.Offset)
	rows, err := tx.QueryContext(ctx, "SELECT id,user_id,category,amount,comment,spent_at,created_at,updated_at FROM payments WHERE "+where+
		fmt.Sprintf(" ORDER BY spent_at DESC,created_at DESC,id DESC LIMIT $%d OFFSET $%d", len(args)-1, len(args)), args...)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var p model.Payment
		if err = rows.Scan(&p.ID, &p.UserID, &p.Category, &p.Amount, &p.Comment, &p.SpentAt, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return result, err
		}
		result.Items = append(result.Items, p)
	}
	if err = rows.Err(); err != nil {
		return result, err
	}
	if err = rows.Close(); err != nil {
		return result, err
	}
	return result, tx.Commit()
}

func (r *Repo) ListUsers(ctx context.Context) ([]string, error) {
	rows, err := r.db.QueryContext(ctx, "SELECT DISTINCT user_id FROM payments ORDER BY user_id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	users := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		users = append(users, id)
	}
	return users, rows.Err()
}
