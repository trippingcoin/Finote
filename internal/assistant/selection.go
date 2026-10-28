package assistant

import (
	"strings"
	"time"

	"finote/internal/payment/model"
)

func selectChoices(answer string, choices []model.Payment, now time.Time, loc *time.Location) []model.Payment {
	now = now.In(loc)
	text := strings.ToLower(strings.Trim(strings.TrimSpace(answer), ".!?"))
	number, _ := referenceNumber(strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(text, "номер "), "№")))
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
	amount, amountOK := parseAmount(strings.TrimSpace(strings.TrimPrefix(text, "на ")))
	var matches []model.Payment
	for _, p := range choices {
		if (date != "" && p.SpentAt.In(loc).Format("2006-01-02") == date) ||
			(strings.HasPrefix(text, "на ") && amountOK && p.Amount == amount) || strings.EqualFold(text, p.ID) {
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
	case "/start", "/new", "новый запрос":
		return false, true
	}
	return false, false
}
