package telegram

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"finote/internal/payment/model"
	"regexp"
	"strconv"
	"strings"
)

type conversationContext struct {
	IDs      []string `json:"ids"`
	Selected string   `json:"selected,omitempty"`
}

func clearContext(ctx context.Context, tx *sql.Tx, bot, user string) error {
	_, err := tx.ExecContext(ctx, "DELETE FROM telegram_context WHERE bot_id=$1 AND user_id=$2", bot, user)
	return err
}
func readContext(ctx context.Context, tx *sql.Tx, bot, user string) (conversationContext, bool, error) {
	var c conversationContext
	var raw []byte
	var expired bool
	err := tx.QueryRowContext(ctx, "SELECT payload,expires_at<=CURRENT_TIMESTAMP FROM telegram_context WHERE bot_id=$1 AND user_id=$2", bot, user).Scan(&raw, &expired)
	if errors.Is(err, sql.ErrNoRows) {
		return c, false, nil
	}
	if err != nil {
		return c, false, err
	}
	if expired {
		return c, false, nil
	}
	err = json.Unmarshal(raw, &c)
	return c, err == nil, err
}
func writeContext(ctx context.Context, tx *sql.Tx, bot, user string, c conversationContext) error {
	raw, err := json.Marshal(c)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO telegram_context(bot_id,user_id,payload,expires_at)
 VALUES($1,$2,$3,CURRENT_TIMESTAMP+INTERVAL '10 minutes')
 ON CONFLICT(bot_id,user_id) DO UPDATE SET payload=EXCLUDED.payload,expires_at=EXCLUDED.expires_at`, bot, user, string(raw))
	return err
}
func rememberList(ctx context.Context, tx *sql.Tx, bot, user string, items []model.Payment) error {
	c := conversationContext{IDs: []string{}}
	for _, p := range items[:min(len(items), 10)] {
		c.IDs = append(c.IDs, p.ID)
	}
	if len(c.IDs) == 1 {
		c.Selected = c.IDs[0]
	}
	return writeContext(ctx, tx, bot, user, c)
}
func rememberSelected(ctx context.Context, tx *sql.Tx, bot, user, id string) error {
	c, _, err := readContext(ctx, tx, bot, user)
	if err != nil {
		return err
	}
	c.Selected = id
	return writeContext(ctx, tx, bot, user, c)
}
func resolveReference(ctx context.Context, tx *sql.Tx, bot, user string, i Intent) (Intent, error) {
	c, found, err := readContext(ctx, tx, bot, user)
	if err != nil {
		return Intent{}, err
	}
	clarify := func(s string) Intent { return Intent{Action: "clarify", Question: s} }
	if !found {
		return clarify("Контекст отсутствует или истёк. Попросите показать список расходов заново."), nil
	}
	id := c.Selected
	if i.ListIndex > 0 {
		if i.ListIndex > len(c.IDs) {
			return clarify("Такого номера нет в последнем списке. Укажите номер из него или запросите новый список."), nil
		}
		id = c.IDs[i.ListIndex-1]
	}
	if id == "" {
		return clarify("Сначала выберите расход, например «покажи второй»."), nil
	}
	i.ID = id
	i.Selected = false
	i.ListIndex = 0
	i.fromReference = true
	return i, nil // execute still checks the real owner in SQL
}
func saveDraft(ctx context.Context, tx *sql.Tx, bot, user string, i Intent) error {
	raw, err := json.Marshal(pendingDialog{Intent: i})
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO telegram_dialogs(bot_id,user_id,payload,expires_at)
 VALUES($1,$2,$3,CURRENT_TIMESTAMP+INTERVAL '10 minutes')
 ON CONFLICT(bot_id,user_id) DO UPDATE SET payload=EXCLUDED.payload,expires_at=EXCLUDED.expires_at`, bot, user, string(raw))
	return err
}

var amountPattern = regexp.MustCompile(`^[0-9]+([.,][0-9]{1,2})?$`)

func parseAmount(text string) (float64, bool) {
	text = strings.TrimSpace(strings.TrimSuffix(strings.TrimSuffix(strings.TrimSpace(text), "₸"), "тенге"))
	if !amountPattern.MatchString(text) {
		return 0, false
	}
	amount, err := strconv.ParseFloat(strings.ReplaceAll(text, ",", "."), 64)
	return amount, err == nil
}

var ordinals = map[string]int{
	"первый": 1, "первую": 1, "первое": 1, "первая": 1,
	"второй": 2, "вторую": 2, "второе": 2, "вторая": 2,
	"третий": 3, "третью": 3, "третье": 3, "третья": 3,
	"четвёртый": 4, "четвертый": 4, "четвёртую": 4, "четвертую": 4,
	"пятый": 5, "пятую": 5, "шестой": 6, "шестую": 6, "седьмой": 7, "седьмую": 7,
	"восьмой": 8, "восьмую": 8, "девятый": 9, "девятую": 9, "десятый": 10, "десятую": 10,
}

func referenceNumber(s string) (int, bool) {
	if n, ok := ordinals[s]; ok {
		return n, true
	}
	n, err := strconv.Atoi(s)
	return n, err == nil
}

// Narrow full-message grammar: an unrecognized command never becomes a mutation.
func referenceCommand(text string) (Intent, bool) {
	text = strings.ToLower(strings.Trim(strings.TrimSpace(text), ".!?"))
	switch text {
	case "а id", "id", "покажи id", "какой id", "а идентификатор":
		return Intent{Action: "id", Selected: true}, true
	}
	text = strings.TrimSuffix(text, " в списке")
	fields := strings.Fields(text)
	if len(fields) < 2 {
		return Intent{}, false
	}
	var action string
	switch fields[0] {
	case "покажи":
		action = "list"
	case "удали":
		action = "delete"
	case "измени":
		action = "update"
	default:
		return Intent{}, false
	}
	target := fields[1]
	if target == "номер" && len(fields) > 2 {
		fields = append(fields[:1], fields[2:]...)
		target = fields[1]
	}
	target = strings.TrimPrefix(target, "№")
	i := Intent{Action: action}
	if target == "его" || target == "её" || target == "ее" || target == "этот" || target == "эту" {
		i.Selected = true
	} else {
		n, ok := referenceNumber(target)
		if !ok {
			return Intent{}, false
		}
		if n < 1 || n > 10 {
			return Intent{Action: "clarify", Question: "Укажите номер от 1 до 10 из последнего списка."}, true
		}
		i.ListIndex = n
	}
	if action == "update" {
		if len(fields) < 4 || fields[2] != "на" {
			return Intent{}, false
		}
		amount, ok := parseAmount(strings.Join(fields[3:], " "))
		if !ok {
			return Intent{Action: "clarify", Question: "Укажите новую сумму, например «измени второй на 700»."}, true
		}
		i.Amount = &amount
	} else if len(fields) != 2 {
		return Intent{}, false
	}
	if err := validate(i); err != nil {
		return Intent{Action: "clarify", Question: "Проверьте сумму и номер расхода."}, true
	}
	return i, true
}
