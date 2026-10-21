package service

import (
	"context"
	"crypto/rand"
	"finote/internal/payment/model"
	"finote/internal/payment/repo"
	"fmt"
	"time"
)

var categories = map[string]struct{}{
	"еда": {}, "транспорт": {}, "жильё": {}, "развлечения": {},
	"здоровье": {}, "покупки": {}, "другое": {},
}

type Service struct {
	repo    *repo.Repo
	newID   func() (string, error)
	newTime func() time.Time
}

func New(repo *repo.Repo) *Service {
	return &Service{repo: repo, newID: newUUID, newTime: time.Now}
}

func (s *Service) CreatePayment(ctx context.Context, body model.Payment) (model.Payment, error) {
	var err error
	body.UserID, err = NormalizeUserID(body.UserID)
	if err != nil {
		return model.Payment{}, err
	}
	if err := validatePayment(body); err != nil {
		return model.Payment{}, err
	}

	now := s.newTime().UTC()
	if body.SpentAt.IsZero() {
		body.SpentAt = now
	}
	if body.SpentAt.Year() < 1900 || body.SpentAt.Year() > 9999 {
		return model.Payment{}, invalid("Укажите корректную дату.")
	}
	body.CreatedAt = now
	body.UpdatedAt = now

	id, err := s.newID()
	if err != nil {
		return model.Payment{}, fmt.Errorf("generate payment id: %w", err)
	}
	body.ID = id

	payment, err := s.repo.CreatePayment(ctx, body)
	if err != nil {
		return model.Payment{}, fmt.Errorf("create payment: %w", err)
	}
	return payment, nil
}

func (s *Service) GetPayment(ctx context.Context, id string) (model.Payment, error) {
	payment, err := s.repo.GetPayment(ctx, id)
	if err != nil {
		return model.Payment{}, fmt.Errorf("get payment: %w", err)
	}
	return payment, nil
}

func (s *Service) UpdatePayment(ctx context.Context, id string, body model.Payment) (model.Payment, error) {
	updatePayment := model.UpdatePayment{
		Category: &body.Category,
		Amount:   &body.Amount,
		Comment:  &body.Comment,
		SpentAt:  &body.SpentAt,
	}
	result, err := s.PatchPayment(ctx, id, updatePayment)
	if err != nil {
		return model.Payment{}, fmt.Errorf("update payment: %w", err)
	}

	return result, nil
}

func (s *Service) DeletePayment(ctx context.Context, id string) error {
	err := s.repo.DeletePayment(ctx, id)
	if err != nil {
		return fmt.Errorf("delete payment: %w", err)
	}
	return nil
}

func newUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}
