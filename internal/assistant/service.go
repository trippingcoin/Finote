// Package assistant coordinates expense commands independently of their transport.
package assistant

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"finote/internal/payment/service"
)

const ContextTTL = 10 * time.Minute

type Profile struct{ DisplayName, Username string }

// Update metadata comes from a verified transport, never from model arguments.
type Update struct {
	BotID, UserID string
	ID            int64
	Profile       *Profile
}

// Request separates application controls from the model's Command.
type Request struct {
	Command Command
	Answer  string
	Cancel  bool
}

type Session struct {
	Payments     *service.Service
	Conversation *Conversation
}

// Store owns the transaction. The callback only runs for a new update, under
// per-update and per-conversation locks. Its reply is committed with the changes.
type Store interface {
	Receipt(context.Context, string, int64) (string, bool, error)
	HasDialog(context.Context, string, string) (bool, error)
	WithUpdate(context.Context, Update, func(context.Context, Session) (string, error)) (string, error)
	QueueReply(context.Context, string, int64, int64, string) error
}

type Service struct {
	planner  Planner
	store    Store
	location *time.Location
	now      func() time.Time
}

func New(planner Planner, store Store, location *time.Location) *Service {
	return &Service{planner: planner, store: store, location: location, now: time.Now}
}

func (s *Service) Handle(ctx context.Context, update Update, text string) (string, error) {
	reply, found, err := s.store.Receipt(ctx, update.BotID, update.ID)
	if err != nil {
		return "", err
	}
	if found {
		// Private chats have the same numeric user and chat IDs. QueueReply is
		// only needed for receipts from releases predating the outbox.
		chat, err := strconv.ParseInt(update.UserID, 10, 64)
		if err != nil {
			return "", err
		}
		return reply, s.store.QueueReply(ctx, update.BotID, update.ID, chat, reply)
	}
	text = strings.TrimSpace(text)
	r := Request{Command: Command{Action: "clarify", Question: "Напишите расход, например «кофе 350», или попросите показать, изменить либо удалить свои траты."}}
	cancel, newRequest := dialogControl(text)
	pending, err := s.store.HasDialog(ctx, update.BotID, update.UserID)
	if err != nil {
		return "", err
	}
	switch {
	case cancel || newRequest:
		r.Cancel = true
		if cancel {
			r.Command.Question = "Уточнение отменено. Ничего не изменено."
		}
	case pending && text != "":
		r.Answer = text
	case text != "":
		if local, ok := referenceCommand(text); ok {
			r.Command = local
		} else {
			parsed, err := s.planner.Plan(ctx, text, s.now().In(s.location))
			if err != nil {
				slog.WarnContext(ctx, "expense command parsing failed", "error", err, "update_id", update.ID)
				r.Command.Question = plannerErrorReply(err)
			} else {
				r.Command = parsed
			}
		}
	}
	return s.Process(ctx, update, r)
}

func (s *Service) Process(ctx context.Context, update Update, request Request) (string, error) {
	if err := request.Command.Validate(); err != nil {
		return "", err
	}
	return s.store.WithUpdate(ctx, update, func(ctx context.Context, session Session) (string, error) {
		i := request.Command
		c := session.Conversation
		if request.Cancel {
			*c = Conversation{}
		} else if request.Answer != "" {
			i = s.resolveDialog(c, request.Answer)
		} else if i.Action != "clarify" {
			c.Pending = nil
		}
		fromReference := i.ListIndex > 0 || i.Selected
		if fromReference {
			i = s.resolveReference(c, i)
		}
		return s.execute(ctx, session, update.UserID, i, fromReference)
	})
}
