package telegram

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"finote/internal/payment/model"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type pendingDialog struct {
	Intent  Intent          `json:"intent"`
	Choices []model.Payment `json:"choices"`
}

func clearDialog(ctx context.Context, tx *sql.Tx, bot, user string) error {
	_, err := tx.ExecContext(ctx, "DELETE FROM telegram_dialogs WHERE bot_id=$1 AND user_id=$2", bot, user)
	return err
}

func (s *Store) HasDialog(ctx context.Context, bot, user string) (bool, error) {
	var found bool
	// Keep expired rows until the next answer so we can explicitly explain expiry.
	err := s.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM telegram_dialogs WHERE bot_id=$1 AND user_id=$2)", bot, user).Scan(&found)
	return found, err
}

func (s *Store) saveDialog(ctx context.Context, tx *sql.Tx, bot, user string, i Intent, choices []model.Payment) (string, error) {
	more := len(choices) > 10
	if more {
		choices = choices[:10]
	}
	data, err := json.Marshal(pendingDialog{i, choices})
	if err != nil {
		return "", err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO telegram_dialogs(bot_id,user_id,payload,expires_at)
 VALUES($1,$2,$3,CURRENT_TIMESTAMP + INTERVAL '10 minutes')
 ON CONFLICT(bot_id,user_id) DO UPDATE SET payload=EXCLUDED.payload,expires_at=EXCLUDED.expires_at`, bot, user, string(data))
	if err != nil {
		return "", err
	}
	var out strings.Builder
	out.WriteString("Найдено несколько трат. Какую выбрать?\n\n")
	for n, p := range choices {
		fmt.Fprintf(&out, "%d. %s\n\n", n+1, s.describe(p))
	}
	if more {
		out.WriteString("Показаны первые 10 совпадений. Для остальных повторите команду с более точным периодом.\n\n")
	}
	out.WriteString("Ответьте номером, например «2», или уточните: «вчерашнюю», «на 1200», «за 20.10.2026». Выбор действует 10 минут. /cancel — отменить. /new — начать новый запрос.")
	return out.String(), nil
}

func (s *Store) resolveDialog(ctx context.Context, tx *sql.Tx, bot, user, answer string) (Intent, error) {
	clarify := func(q string) Intent { return Intent{Action: "clarify", Question: q} }
	var raw []byte
	var expired bool
	err := tx.QueryRowContext(ctx, `SELECT payload,expires_at<=CURRENT_TIMESTAMP FROM telegram_dialogs WHERE bot_id=$1 AND user_id=$2`, bot, user).Scan(&raw, &expired)
	if errors.Is(err, sql.ErrNoRows) {
		return clarify("Нет активного уточнения. Напишите команду заново."), nil
	}
	if err != nil {
		return Intent{}, err
	}
	if expired {
		return clarify("Время уточнения истекло. Ничего не изменено. Напишите команду заново."), clearDialog(ctx, tx, bot, user)
	}
	var pending pendingDialog
	if err = json.Unmarshal(raw, &pending); err != nil {
		return Intent{}, err
	}
	selected := selectChoices(answer, pending.Choices, time.Now().In(s.location), s.location)
	if len(selected) != 1 {
		return clarify("Не удалось однозначно выбрать трату. Ничего не изменено. Ответьте номером из списка, датой или суммой. /cancel — отменить, /new — новый запрос."), nil
	}
	i := pending.Intent
	i.ID = selected[0].ID
	i.Latest = false
	// The original values for update are preserved; selection never supplies new values.
	i.Search = ""
	i.FilterCategory = ""
	i.From = ""
	i.To = ""
	if i.Action != "update" && i.Action != "delete" {
		return Intent{}, errors.New("invalid pending operation")
	}
	if err = validate(i); err != nil {
		return Intent{}, err
	}
	return i, clearDialog(ctx, tx, bot, user)
}

func selectChoices(answer string, choices []model.Payment, now time.Time, loc *time.Location) []model.Payment {
	text := strings.ToLower(strings.Trim(strings.TrimSpace(answer), ".!?"))
	ordinals := map[string]int{"первую": 1, "первая": 1, "первый": 1, "вторую": 2, "вторая": 2, "второй": 2, "третью": 3, "третья": 3, "третий": 3}
	number, ordinal := ordinals[text]
	if !ordinal {
		number, _ = strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(text, "номер "), "№")))
	}
	if number > 0 {
		if number <= len(choices) {
			return []model.Payment{choices[number-1]}
		}
		return nil
	}
	date := ""
	switch text {
	case "вчера", "вчерашнюю", "вчерашняя":
		date = now.AddDate(0, 0, -1).Format("2006-01-02")
	case "сегодня", "сегодняшнюю", "сегодняшняя":
		date = now.Format("2006-01-02")
	case "позавчера", "позавчерашнюю":
		date = now.AddDate(0, 0, -2).Format("2006-01-02")
	}
	for _, format := range []string{"02.01.2006", "2006-01-02"} {
		if d, err := time.Parse(format, strings.TrimPrefix(text, "за ")); err == nil {
			date = d.Format("2006-01-02")
		}
	}
	amountText := strings.TrimSpace(strings.TrimSuffix(strings.TrimSuffix(strings.TrimPrefix(text, "на "), "тенге"), "₸"))
	amount, amountErr := strconv.ParseFloat(strings.ReplaceAll(amountText, ",", "."), 64)
	var matches []model.Payment
	for _, p := range choices {
		if (date != "" && p.SpentAt.In(loc).Format("2006-01-02") == date) ||
			(strings.HasPrefix(text, "на ") && amountErr == nil && p.Amount == amount) || strings.EqualFold(text, p.ID) {
			matches = append(matches, p)
		}
	}
	return matches
}

// New commands must be explicit while a destructive operation awaits a choice.
func dialogControl(text string) (cancel, newRequest bool) {
	switch strings.ToLower(strings.TrimSpace(text)) {
	case "/cancel", "отмена", "отмени":
		return true, false
	case "/start", "/new":
		return false, true
	}
	return false, false
}
