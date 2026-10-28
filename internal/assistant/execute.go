package assistant

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"finote/internal/payment/model"
)

func (s *Service) execute(ctx context.Context, session Session, user string, i Command, fromReference bool) (string, error) {
	if i.Action == "draft_create" {
		s.saveDraft(session.Conversation, i)
		return fmt.Sprintf("Какую сумму записать для «%s»? Ответьте числом, например «400». Уточнение действует 10 минут. /cancel — отменить.", trim(*i.Comment, 100)), nil
	}
	if i.Action == "clarify" {
		if i.Question == "" {
			return "Уточните действие, сумму или нужную трату.", nil
		}
		return trim(i.Question, 1000), nil
	}
	payments := session.Payments
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
		s.rememberSelected(session.Conversation, p.ID)
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
		if fromReference {
			return "Эта трата удалена или недоступна. Попросите показать список заново.", nil
		}
		if i.Action == "list" {
			s.rememberList(session.Conversation, nil)
			return "Подходящих трат не найдено.", nil
		}
		return "Подходящих трат не найдено.", nil
	}
	if i.Action != "list" && len(found) > 1 {
		return s.saveDialog(session.Conversation, i, found), nil
	}
	if i.Action == "list" {
		if fromReference {
			s.rememberSelected(session.Conversation, found[0].ID)
		} else {
			s.rememberList(session.Conversation, found)
		}
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
	if i.Action == "id" {
		s.rememberSelected(session.Conversation, id)
		return "ID расхода: " + id + "\n\n" + s.describe(found[0]), nil
	}
	// Owner is checked again in the actual mutation, never only in a prior SELECT.
	if i.Action == "delete" {
		err := payments.DeletePayment(ctx, id)
		if errors.Is(err, sql.ErrNoRows) {
			return "Трата уже удалена.", nil
		}
		if err != nil {
			return "", err
		}
		c := session.Conversation
		active := s.now().Before(c.ContextExpiresAt)
		if active && c.Context.Selected == id {
			c.Context.Selected = ""
			c.ContextExpiresAt = s.now().Add(ContextTTL)
		}
		return "Расход удалён\n\n" + s.describe(found[0]), err
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
	s.rememberSelected(session.Conversation, p.ID)
	return "Расход обновлён\n\n" + s.describe(p), nil
}

func (s *Service) paymentFilter(i Command) model.Filter {
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

func (s *Service) describe(p model.Payment) string {
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
