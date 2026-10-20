package service

import (
	"context"
	"errors"
	"finote/internal/payment/model"
	"fmt"
	"math"
	"strconv"
	"strings"
)

var ErrValidation = errors.New("invalid payment")

func invalid(message string) error { return fmt.Errorf("%w: %s", ErrValidation, message) }

func ValidateCategory(category string) bool { _, ok := categories[category]; return ok }

func validateAmount(amount float64) error {
	if math.IsNaN(amount) || math.IsInf(amount, 0) || amount < 0.01 || amount >= 1e12 {
		return invalid("Сумма должна быть от 0,01 до 999 999 999 999,99.")
	}
	if math.Abs(amount*100-math.Round(amount*100)) > 0.001 {
		return invalid("Укажите не более двух знаков после запятой.")
	}
	return nil
}

func validatePayment(p model.Payment) error {
	id, err := strconv.ParseInt(p.UserID, 10, 64)
	if err != nil || id <= 0 {
		return invalid("Укажите положительный Telegram ID участника.")
	}
	if err := validateAmount(p.Amount); err != nil {
		return err
	}
	if !ValidateCategory(p.Category) {
		return invalid("Выберите допустимую категорию.")
	}
	if len([]rune(p.Comment)) > 500 {
		return invalid("Комментарий должен быть не длиннее 500 символов.")
	}
	return nil
}

func (s *Service) PatchPayment(ctx context.Context, id string, p model.UpdatePayment) (model.Payment, error) {
	if p.Amount == nil && p.Category == nil && p.Comment == nil && p.SpentAt == nil {
		return model.Payment{}, invalid("Передайте хотя бы одно изменяемое поле.")
	}
	if p.Amount != nil {
		if err := validateAmount(*p.Amount); err != nil {
			return model.Payment{}, err
		}
	}
	if p.Category != nil && !ValidateCategory(*p.Category) {
		return model.Payment{}, invalid("Выберите допустимую категорию.")
	}
	if p.Comment != nil && len([]rune(*p.Comment)) > 500 {
		return model.Payment{}, invalid("Комментарий должен быть не длиннее 500 символов.")
	}
	if p.SpentAt != nil && (p.SpentAt.IsZero() || p.SpentAt.Year() < 1900 || p.SpentAt.Year() > 9999) {
		return model.Payment{}, invalid("Укажите корректную дату.")
	}
	return s.repo.UpdatePayment(ctx, id, p, s.newTime().UTC())
}

func (s *Service) ListPayments(ctx context.Context, f model.Filter) (model.PaymentList, error) {
	return s.repo.ListPayments(ctx, f)
}
func (s *Service) ListUsers(ctx context.Context) ([]string, error) { return s.repo.ListUsers(ctx) }

// Normalizes only whitespace; ownership still comes from the supplied admin input.
func normalizePayment(p model.Payment) model.Payment {
	p.UserID = strings.TrimSpace(p.UserID)
	return p
}
