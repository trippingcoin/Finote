package telegram

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"finote/internal/migrations"
	"finote/internal/payment/model"
	"finote/internal/payment/repo"
	"finote/internal/payment/service"
)

type Store struct {
	db       *sql.DB
	location *time.Location
}

const databaseTimeout = 5 * time.Second

func NewStore(db *sql.DB, location *time.Location) *Store { return &Store{db, location} }

func (s *Store) Init(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	return migrations.Apply(ctx, s.db)
}

func (s *Store) Receipt(ctx context.Context, bot string, update int64) (string, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, databaseTimeout)
	defer cancel()
	var reply string
	err := s.db.QueryRowContext(ctx, "SELECT response FROM telegram_updates WHERE bot_id=$1 AND update_id=$2", bot, update).Scan(&reply)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return reply, err == nil, err
}

// Mutation and receipt commit together; redelivery cannot repeat a mutation.
func (s *Store) Process(ctx context.Context, bot string, update int64, user string, i Intent) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, databaseTimeout)
	defer cancel()
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
		if _, err = tx.ExecContext(ctx, enqueueReplySQL, bot, update, user, reply); err != nil {
			return "", err
		}
		return reply, tx.Commit()
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
	if _, err = tx.ExecContext(ctx, enqueueReplySQL, bot, update, user, reply); err != nil {
		return "", err
	}
	return reply, tx.Commit()
}

func (s *Store) execute(ctx context.Context, tx *sql.Tx, bot, user string, i Intent) (string, error) {
	if i.Action == "clarify" {
		if i.Question == "" {
			return "Уточните действие, сумму или нужную трату.", nil
		}
		return trim(i.Question, 1000), nil
	}
	owned, err := repo.NewOwnedTx(tx, user)
	if err != nil {
		return "", err
	}
	payments := service.New(owned)
	if i.Action == "create" {
		var spent time.Time
		if i.SpentAt != nil {
			spent, _ = time.Parse(time.RFC3339, *i.SpentAt)
		}
		comment := ""
		if i.Comment != nil {
			comment = *i.Comment
		}
		p, err := payments.CreatePayment(ctx, model.Payment{UserID: user, Category: *i.Category, Amount: *i.Amount, Comment: comment, SpentAt: spent})
		if err != nil {
			return "", err
		}
		return "Расход добавлен\n\n" + s.describe(p), nil
	}
	filter := s.paymentFilter(i)
	if i.Action == "sum" {
		total, count, err := payments.SumPayments(ctx, filter)
		return fmt.Sprintf("Сумма расходов: %s ₸\nТрат: %d", total, count), err
	}
	found, err := payments.FindPayments(ctx, filter)
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
		err := payments.DeletePayment(ctx, id)
		if errors.Is(err, sql.ErrNoRows) {
			return "Трата уже удалена.", nil
		}
		if err != nil {
			return "", err
		}
		return "Расход удалён\n\n" + s.describe(found[0]), nil
	}
	changes, err := i.changes()
	if err != nil {
		return "", err
	}
	p, err := payments.PatchPayment(ctx, id, changes)
	if errors.Is(err, sql.ErrNoRows) {
		return "Трата уже удалена.", nil
	}
	if err != nil {
		return "", err
	}
	return "Расход обновлён\n\n" + s.describe(p), nil
}

func (s *Store) paymentFilter(i Intent) model.Filter {
	f := model.Filter{ID: i.ID, Search: i.Search, Category: i.FilterCategory, Limit: 11}
	if i.Latest {
		f.Limit = 1
	}
	if i.From != "" {
		t, _ := time.ParseInLocation("2006-01-02", i.From, s.location)
		f.From = &t
	}
	if i.To != "" {
		t, _ := time.ParseInLocation("2006-01-02", i.To, s.location)
		f.To = &t
	}
	return f
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
