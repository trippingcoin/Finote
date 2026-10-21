package repo

import (
	"context"
	"database/sql"
	"finote/internal/payment/model"
	"fmt"
	"strings"
)

const columns = "id,user_id,category,amount,comment,spent_at,created_at,updated_at"

func (r *Repo) filter(f model.Filter) (string, []any) {
	if r.owner != "" {
		f.UserID = r.owner
	}
	parts := []string{"TRUE"}
	args := []any{}
	add := func(expr string, value any) {
		args = append(args, value)
		parts = append(parts, fmt.Sprintf(expr, len(args)))
	}
	if f.UserID != "" {
		add("user_id=$%d", f.UserID)
	}
	if f.ID != "" {
		add("id=$%d", f.ID)
	}
	if f.Search != "" {
		add("strpos(lower(comment),lower($%d))>0", f.Search)
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
	return strings.Join(parts, " AND "), args
}

func scanRows(rows *sql.Rows) ([]model.Payment, error) {
	defer rows.Close()
	items := []model.Payment{}
	for rows.Next() {
		var p model.Payment
		if err := rows.Scan(&p.ID, &p.UserID, &p.Category, &p.Amount, &p.Comment, &p.SpentAt, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, p)
	}
	return items, rows.Err()
}

func (r *Repo) ListPayments(ctx context.Context, f model.Filter) (model.PaymentList, error) {
	if r.db == nil {
		return r.list(ctx, r.query, f)
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return model.PaymentList{}, err
	}
	defer tx.Rollback()
	result, err := r.list(ctx, tx, f)
	if err != nil {
		return result, err
	}
	return result, tx.Commit()
}

func (r *Repo) list(ctx context.Context, q DBTX, f model.Filter) (model.PaymentList, error) {
	result := model.PaymentList{Items: []model.Payment{}, Limit: f.Limit, Offset: f.Offset}
	where, args := r.filter(f)
	err := q.QueryRowContext(ctx, "SELECT COUNT(*),COALESCE(SUM(amount),0)::text,COUNT(DISTINCT user_id) FROM payments WHERE "+where, args...).Scan(&result.Total, &result.Amount, &result.Members)
	if err != nil {
		return result, err
	}
	args = append(args, f.Limit, f.Offset)
	rows, err := q.QueryContext(ctx, "SELECT "+columns+" FROM payments WHERE "+where+fmt.Sprintf(" ORDER BY spent_at DESC,created_at DESC,id DESC LIMIT $%d OFFSET $%d", len(args)-1, len(args)), args...)
	if err != nil {
		return result, err
	}
	result.Items, err = scanRows(rows)
	return result, err
}

// FindPayments keeps the bot's latest-by-creation semantics, not panel date order.
func (r *Repo) FindPayments(ctx context.Context, f model.Filter) ([]model.Payment, error) {
	where, args := r.filter(f)
	limit := f.Limit
	if limit < 1 || limit > 100 {
		limit = 11
	}
	args = append(args, limit)
	rows, err := r.query.QueryContext(ctx, "SELECT "+columns+" FROM payments WHERE "+where+fmt.Sprintf(" ORDER BY created_at DESC,id DESC LIMIT $%d", len(args)), args...)
	if err != nil {
		return nil, err
	}
	return scanRows(rows)
}

func (r *Repo) SumPayments(ctx context.Context, f model.Filter) (string, int64, error) {
	where, args := r.filter(f)
	var total string
	var count int64
	err := r.query.QueryRowContext(ctx, "SELECT COALESCE(SUM(amount),0)::text,COUNT(*) FROM payments WHERE "+where, args...).Scan(&total, &count)
	return total, count, err
}

func (r *Repo) ListUsers(ctx context.Context) ([]string, error) {
	where, args := r.filter(model.Filter{})
	rows, err := r.query.QueryContext(ctx, "SELECT DISTINCT user_id FROM payments WHERE "+where+" ORDER BY user_id", args...)
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
