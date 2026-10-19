package service

import (
	"context"
	"crypto/rand"
	"errors"
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
	if body.UserID == "" {
		return model.Payment{}, errors.New("user_id is required")
	}
	if body.Amount <= 0 {
		return model.Payment{}, errors.New("amount must be greater than zero")
	}
	if _, ok := categories[body.Category]; !ok {
		return model.Payment{}, errors.New("invalid category")
	}

	now := s.newTime().UTC()
	if body.SpentAt.IsZero() {
		body.SpentAt = now
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
	_, err := s.repo.GetPayment(ctx, id)
	if err != nil {
		return model.Payment{}, fmt.Errorf("get payment: %w", err)
	}

	updatePayment := model.UpdatePayment{
		Category: &body.Category,
		Amount:   &body.Amount,
		Comment:  &body.Comment,
		SpentAt:  &body.SpentAt,
	}
	result, err := s.repo.UpdatePayment(ctx, id, updatePayment, s.newTime().UTC())
	if err != nil {
		return model.Payment{}, fmt.Errorf("update payment: %w", err)
	}

	return result, nil
}

func (s *Service) DeletePayment(ctx context.Context, id string) error {
	_, err := s.repo.GetPayment(ctx, id)
	if err != nil {
		return fmt.Errorf("get payment: %w", err)
	}
	err = s.repo.DeletePayment(ctx, id)
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
