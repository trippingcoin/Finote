package assistant

import (
	"fmt"
	"strings"
	"time"

	"finote/internal/payment/model"
)

// Conversation unifies pending input and remembered references. Independent
// expiry times preserve the meaning of lists while a clarification is active.
type Conversation struct {
	Context          ConversationContext
	ContextExpiresAt time.Time
	Pending          *PendingDialog
	PendingExpiresAt time.Time
}

type ConversationContext struct {
	IDs      []string `json:"ids"`
	Selected string   `json:"selected,omitempty"`
}

type PendingDialog struct {
	// Keep the legacy payload name so pending dialogs survive the upgrade.
	Command Command         `json:"intent"`
	Choices []model.Payment `json:"choices"`
}

func (s *Service) rememberList(c *Conversation, items []model.Payment) {
	c.Context = ConversationContext{IDs: []string{}}
	for _, p := range items[:min(len(items), 10)] {
		c.Context.IDs = append(c.Context.IDs, p.ID)
	}
	if len(c.Context.IDs) == 1 {
		c.Context.Selected = c.Context.IDs[0]
	}
	c.ContextExpiresAt = s.now().Add(ContextTTL)
}

func (s *Service) rememberSelected(c *Conversation, id string) {
	if !s.now().Before(c.ContextExpiresAt) {
		c.Context = ConversationContext{}
	}
	c.Context.Selected = id
	c.ContextExpiresAt = s.now().Add(ContextTTL)
}

func (s *Service) resolveReference(c *Conversation, i Command) Command {
	clarify := func(q string) Command { return Command{Action: "clarify", Question: q} }
	if !s.now().Before(c.ContextExpiresAt) {
		return clarify("Контекст отсутствует или истёк. Попросите показать список расходов заново.")
	}
	id := c.Context.Selected
	if i.ListIndex > 0 {
		if i.ListIndex > len(c.Context.IDs) {
			return clarify("Такого номера нет в последнем списке. Укажите номер из него или запросите новый список.")
		}
		id = c.Context.IDs[i.ListIndex-1]
	}
	if id == "" {
		return clarify("Сначала выберите расход, например «покажи второй».")
	}
	i.ID, i.Selected, i.ListIndex = id, false, 0
	return i
}

func (s *Service) saveDraft(c *Conversation, i Command) {
	c.Pending = &PendingDialog{Command: i}
	c.PendingExpiresAt = s.now().Add(ContextTTL)
}

func (s *Service) saveDialog(c *Conversation, i Command, choices []model.Payment) string {
	more := len(choices) > 10
	choices = choices[:min(len(choices), 10)]
	c.Pending = &PendingDialog{Command: i, Choices: choices}
	c.PendingExpiresAt = s.now().Add(ContextTTL)
	var out strings.Builder
	out.WriteString("Найдено несколько трат. Какую выбрать?\n\n")
	for n, p := range choices {
		fmt.Fprintf(&out, "%d. %s\n\n", n+1, s.describe(p))
	}
	if more {
		out.WriteString("Показаны первые 10 совпадений. Для остальных повторите команду с более точным периодом.\n\n")
	}
	out.WriteString("Ответьте номером, например «2», или уточните: «вчерашнюю», «на 1200», «за 20.10.2026». Выбор действует 10 минут. /cancel — отменить. /new — начать новый запрос.")
	return out.String()
}

func (s *Service) resolveDialog(c *Conversation, answer string) Command {
	clarify := func(q string) Command { return Command{Action: "clarify", Question: q} }
	if c.Pending == nil {
		return clarify("Нет активного уточнения. Напишите команду заново.")
	}
	if !s.now().Before(c.PendingExpiresAt) {
		c.Pending = nil
		return clarify("Время уточнения истекло. Ничего не изменено. Напишите команду заново.")
	}
	i := c.Pending.Command
	if i.Action == "draft_create" {
		amount, ok := parseAmount(answer)
		if !ok {
			return clarify("Укажите сумму числом, например «400» или «400,50». Для нового запроса напишите «отмена».")
		}
		i.Action, i.Amount = "create", &amount
		if err := i.Validate(); err != nil {
			return clarify("Сумма должна быть положительной, не более двух знаков после запятой.")
		}
	} else {
		selected := selectChoices(answer, c.Pending.Choices, s.now(), s.location)
		if len(selected) != 1 {
			return clarify("Не удалось однозначно выбрать трату. Ничего не изменено. Ответьте номером из списка, датой или суммой. /cancel — отменить, /new — новый запрос.")
		}
		i.ID, i.Latest = selected[0].ID, false
		i.Search, i.FilterCategory, i.From, i.To = "", "", "", ""
		if (i.Action != "update" && i.Action != "delete") || i.Validate() != nil {
			return clarify("Не удалось восстановить действие. Отмените уточнение и напишите запрос заново.")
		}
	}
	c.Pending = nil
	return i
}
