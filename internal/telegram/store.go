package telegram

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"finote/internal/payment/model"
)

type Store struct {
	db       *sql.DB
	location *time.Location
}

func NewStore(db *sql.DB, location *time.Location) *Store { return &Store{db, location} }

func (s *Store) Init(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS telegram_updates (
 bot_id TEXT NOT NULL, update_id BIGINT NOT NULL, response TEXT NOT NULL,
 PRIMARY KEY (bot_id, update_id)
 )`)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS telegram_dialogs (
 bot_id TEXT NOT NULL, user_id TEXT NOT NULL, payload JSONB NOT NULL,
 expires_at TIMESTAMPTZ NOT NULL, PRIMARY KEY(bot_id,user_id)
 )`)
	return err
}

func (s *Store) Offset(ctx context.Context, bot string) (int64, error) {
	var offset int64
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(update_id)+1,0) FROM telegram_updates WHERE bot_id=$1`, bot).Scan(&offset)
	return offset, err
}

// Mutation and receipt commit together; redelivery cannot repeat a mutation.
func (s *Store) Process(ctx context.Context, bot string, update int64, user string, i Intent) (string, error) {
	if user == "" {
		return "", errors.New("missing owner")
	}
	if err := validate(i); err != nil {
		return "", err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	// Serialize concurrent deliveries of this update before checking the receipt.
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, fmt.Sprintf("%s:%d", bot, update)); err != nil {
		return "", err
	}
	var reply string
	err = tx.QueryRowContext(ctx, `SELECT response FROM telegram_updates WHERE bot_id=$1 AND update_id=$2`, bot, update).Scan(&reply)
	if err == nil {
		return reply, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	// Serialize a user's conversation as well as individual update delivery.
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "dialog:"+bot+":"+user); err != nil {
		return "", err
	}
	if i.cancelDialog {
		err = clearDialog(ctx, tx, bot, user)
	} else if i.answer != "" {
		i, err = s.resolveDialog(ctx, tx, bot, user, i.answer)
	} else if i.Action != "clarify" {
		err = clearDialog(ctx, tx, bot, user)
	}
	if err != nil {
		return "", err
	}
	reply, err = s.execute(ctx, tx, bot, user, i)
	if err != nil {
		return "", err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO telegram_updates(bot_id,update_id,response) VALUES($1,$2,$3)`, bot, update, reply); err != nil {
		return "", err
	}
	return reply, tx.Commit()
}

const columns = "id, user_id, category, amount, comment, spent_at, created_at, updated_at"

type scanner interface{ Scan(...any) error }

func scanPayment(row scanner) (model.Payment, error) {
	var p model.Payment
	err := row.Scan(&p.ID, &p.UserID, &p.Category, &p.Amount, &p.Comment, &p.SpentAt, &p.CreatedAt, &p.UpdatedAt)
	return p, err
}

func (s *Store) execute(ctx context.Context, tx *sql.Tx, bot, user string, i Intent) (string, error) {
	if i.Action == "clarify" {
		if i.Question == "" {
			return "Уточните действие, сумму или нужную трату.", nil
		}
		return trim(i.Question, 1000), nil
	}
	now := time.Now().UTC()
	if i.Action == "create" {
		spent := now
		if i.SpentAt != nil {
			spent, _ = time.Parse(time.RFC3339, *i.SpentAt)
		}
		comment := ""
		if i.Comment != nil {
			comment = *i.Comment
		}
		var b [16]byte
		if _, err := rand.Read(b[:]); err != nil {
			return "", err
		}
		b[6] = (b[6] & 15) | 64
		b[8] = (b[8] & 63) | 128
		id := fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:])
		p, err := scanPayment(tx.QueryRowContext(ctx, `INSERT INTO payments(id,user_id,category,amount,comment,spent_at,created_at,updated_at)
   VALUES($1,$2,$3,$4,$5,$6,$7,$7) RETURNING `+columns, id, user, i.Category, i.Amount, comment, spent, now))
		if err != nil {
			return "", err
		}
		return "Расход добавлен\n\n" + s.describe(p), nil
	}
	where, args := s.filter(user, i)
	if i.Action == "sum" {
		var total string
		var count int64
		err := tx.QueryRowContext(ctx, "SELECT COALESCE(SUM(amount),0)::text,COUNT(*) FROM payments WHERE "+where, args...).Scan(&total, &count)
		return fmt.Sprintf("Сумма расходов: %s ₸\nТрат: %d", total, count), err
	}
	limit := 11
	if i.Latest {
		limit = 1
	}
	rows, err := tx.QueryContext(ctx, "SELECT "+columns+" FROM payments WHERE "+where+
		fmt.Sprintf(" ORDER BY created_at DESC,id DESC LIMIT %d", limit), args...)
	if err != nil {
		return "", err
	}
	var found []model.Payment
	for rows.Next() {
		p, e := scanPayment(rows)
		if e != nil {
			rows.Close()
			return "", e
		}
		found = append(found, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return "", err
	}
	if len(found) == 0 {
		return "Подходящих трат не найдено.", nil
	}
	if i.Action != "list" && len(found) > 1 {
		return s.saveDialog(ctx, tx, bot, user, i, found)
	}
	if i.Action == "list" {
		var out strings.Builder
		out.WriteString("Ваши расходы\n\n")
		for n, p := range found {
			if n == 10 {
				out.WriteString("Есть ещё записи — уточните период или описание.\n")
				break
			}
			fmt.Fprintf(&out, "%d. %s", n+1, s.describe(p))
			out.WriteString("\n\n")
		}
		return out.String(), nil
	}
	id := found[0].ID
	// Owner is checked again in the actual mutation, never only in a prior SELECT.
	if i.Action == "delete" {
		res, err := tx.ExecContext(ctx, "DELETE FROM payments WHERE id=$1 AND user_id=$2", id, user)
		if err != nil {
			return "", err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return "", err
		}
		if n == 0 {
			return "Трата уже удалена.", nil
		}
		return "Расход удалён\n\n" + s.describe(found[0]), nil
	}
	var spent *time.Time
	if i.SpentAt != nil {
		t, _ := time.Parse(time.RFC3339, *i.SpentAt)
		spent = &t
	}
	p, err := scanPayment(tx.QueryRowContext(ctx, `UPDATE payments SET
 category=COALESCE($3,category),amount=COALESCE($4,amount),
 comment=COALESCE($5,comment),spent_at=COALESCE($6,spent_at),updated_at=$7
 WHERE id=$1 AND user_id=$2 RETURNING `+columns, id, user, i.Category, i.Amount, i.Comment, spent, now))
	if errors.Is(err, sql.ErrNoRows) {
		return "Трата уже удалена.", nil
	}
	if err != nil {
		return "", err
	}
	return "Расход обновлён\n\n" + s.describe(p), nil
}

func (s *Store) filter(user string, i Intent) (string, []any) {
	parts := []string{"user_id=$1"}
	args := []any{user}
	add := func(expression string, value any) {
		args = append(args, value)
		parts = append(parts, fmt.Sprintf(expression, len(args)))
	}
	if i.ID != "" {
		add("id=$%d", i.ID)
	}
	if i.Search != "" {
		add("strpos(lower(comment),lower($%d))>0", i.Search)
	}
	if i.FilterCategory != "" {
		add("category=$%d", i.FilterCategory)
	}
	if i.From != "" {
		t, _ := time.ParseInLocation("2006-01-02", i.From, s.location)
		add("spent_at >= $%d", t)
	}
	if i.To != "" {
		t, _ := time.ParseInLocation("2006-01-02", i.To, s.location)
		add("spent_at < $%d", t)
	}
	return strings.Join(parts, " AND "), args
}

func (s *Store) describe(p model.Payment) string {
	description := strings.Join(strings.Fields(p.Comment), " ")
	if description == "" {
		description = "Без описания"
	}
	return fmt.Sprintf("%s\nСумма: %s ₸\nКатегория: %s\nДата: %s",
		trim(description, 100), strconv.FormatFloat(p.Amount, 'f', -1, 64),
		p.Category, p.SpentAt.In(s.location).Format("02.01.2006"))
}
func trim(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}
