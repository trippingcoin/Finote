package assistant

import (
	"context"
	"errors"
	"strings"
	"time"

	"finote/internal/payment/model"
	"finote/internal/payment/service"
)

// Command contains no owner field: ownership always comes from Telegram.
type Command struct {
	ListIndex      int      `json:"list_index,omitempty"`
	Selected       bool     `json:"selected,omitempty"`
	Action         string   `json:"action"`
	ID             string   `json:"id"`
	Latest         bool     `json:"latest"`
	Search         string   `json:"search"`
	FilterCategory string   `json:"filter_category"`
	From           string   `json:"from"`
	To             string   `json:"to"`
	Category       *string  `json:"category"`
	Amount         *float64 `json:"amount"`
	Comment        *string  `json:"comment"`
	SpentAt        *string  `json:"spent_at"`
	Question       string   `json:"question"`
}

type Planner interface {
	Plan(context.Context, string, time.Time) (Command, error)
}

func validCategory(s string) bool {
	return service.ValidateCategory(s)
}

func (i Command) changes() (model.UpdatePayment, error) {
	p := model.UpdatePayment{Amount: i.Amount, Category: i.Category, Comment: i.Comment}
	if i.SpentAt != nil {
		t, err := time.Parse(time.RFC3339, *i.SpentAt)
		if err != nil {
			return p, err
		}
		p.SpentAt = &t
	}
	return p, nil
}

func (i Command) Validate() error {
	bad := errors.New("invalid expense intent")
	ref := i.ListIndex != 0 || i.Selected
	if i.ListIndex < 0 || i.ListIndex > 10 {
		return bad
	}
	if ref {
		if (i.ListIndex != 0 && i.Selected) || i.ID != "" || i.Latest || i.Search != "" || i.FilterCategory != "" || i.From != "" || i.To != "" {
			return bad
		}
		if i.Action != "list" && i.Action != "update" && i.Action != "delete" && i.Action != "id" {
			return bad
		}
	}
	switch i.Action {
	case "draft_create":
		if i.Amount != nil || i.Category == nil || i.Comment == nil || strings.TrimSpace(*i.Comment) == "" {
			return bad
		}
	case "id":
		if !ref && i.ID == "" {
			return bad
		}
	case "create":
		if i.Amount == nil || i.Category == nil {
			return bad
		}
	case "update":
		if i.Amount == nil && i.Category == nil && i.Comment == nil && i.SpentAt == nil {
			return bad
		}
		fallthrough
	case "delete":
		if !ref && i.ID == "" && !i.Latest && i.Search == "" && i.FilterCategory == "" && i.From == "" && i.To == "" {
			return bad
		}
	case "list", "sum", "clarify":
	default:
		return bad
	}
	if i.Amount != nil || i.Category != nil || i.Comment != nil || i.SpentAt != nil {
		changes, err := i.changes()
		if err != nil {
			return bad
		}
		if err = service.ValidateChanges(changes); err != nil {
			return err
		}
	}
	if i.FilterCategory != "" && !validCategory(i.FilterCategory) {
		return bad
	}
	if len(i.Search) > 500 {
		return bad
	}
	if i.ID != "" {
		if len(i.ID) != 36 {
			return bad
		}
		for n, c := range i.ID {
			if n == 8 || n == 13 || n == 18 || n == 23 {
				if c != '-' {
					return bad
				}
				continue
			}
			if !strings.ContainsRune("0123456789abcdefABCDEF", c) {
				return bad
			}
		}
	}
	for _, date := range []string{i.From, i.To} {
		if date != "" {
			if parsed, err := time.Parse("2006-01-02", date); err != nil || parsed.Year() < 1 {
				return bad
			}
		}
	}
	if i.From != "" && i.To != "" && i.From >= i.To {
		return bad
	}
	return nil
}
