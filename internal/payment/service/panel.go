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
	// Inspect the shortest decimal representation instead of accepting a rounding
	// tolerance that silently lets amounts such as 1.000001 reach NUMERIC(14,2).
	decimal := strconv.FormatFloat(amount, 'f', -1, 64)
	if dot := strings.IndexByte(decimal, '.'); dot >= 0 && len(decimal)-dot-1 > 2 {
		return invalid("Укажите не более двух знаков после запятой.")
	}
	return nil
}

func validatePayment(p model.Payment) error {
	if _, err := NormalizeUserID(p.UserID); err != nil {
		return err
	}
	return ValidateChanges(model.UpdatePayment{Amount: &p.Amount, Category: &p.Category, Comment: &p.Comment})
}

func (s *Service) PatchPayment(ctx context.Context, id string, p model.UpdatePayment) (model.Payment, error) {
	if err := ValidateChanges(p); err != nil {
		return model.Payment{}, err
	}
	return s.repo.UpdatePayment(ctx, id, p, s.newTime().UTC())
}

// ValidateChanges is shared by HTTP updates and parsed Telegram intents.
func ValidateChanges(p model.UpdatePayment) error {
	if p.Amount == nil && p.Category == nil && p.Comment == nil && p.SpentAt == nil {
		return invalid("Передайте хотя бы одно изменяемое поле.")
	}
	if p.Amount != nil {
		if err := validateAmount(*p.Amount); err != nil {
			return err
		}
	}
	if p.Category != nil && !ValidateCategory(*p.Category) {
		return invalid("Выберите допустимую категорию.")
	}
	if p.Comment != nil && len([]rune(*p.Comment)) > 500 {
		return invalid("Комментарий должен быть не длиннее 500 символов.")
	}
	if p.SpentAt != nil && (p.SpentAt.IsZero() || p.SpentAt.Year() < 1900 || p.SpentAt.Year() > 9999) {
		return invalid("Укажите корректную дату.")
	}
	return nil
}

func (s *Service) FindPayments(ctx context.Context, f model.Filter) ([]model.Payment, error) {
	f, err := normalizeFilter(f)
	if err != nil {
		return nil, err
	}
	return s.repo.FindPayments(ctx, f)
}
func (s *Service) SumPayments(ctx context.Context, f model.Filter) (string, int64, error) {
	f, err := normalizeFilter(f)
	if err != nil {
		return "", 0, err
	}
	return s.repo.SumPayments(ctx, f)
}

func (s *Service) ListPayments(ctx context.Context, f model.Filter) (model.PaymentList, error) {
	f, err := normalizeFilter(f)
	if err != nil {
		return model.PaymentList{}, err
	}
	return s.repo.ListPayments(ctx, f)
}
func (s *Service) ListUsers(ctx context.Context) ([]string, error) { return s.repo.ListUsers(ctx) }

// NormalizeUserID matches Telegram's decimal int64 representation.
// This changes formatting, not the source of ownership.
func NormalizeUserID(value string) (string, error) {
	id, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil || id <= 0 {
		return "", invalid("Укажите положительный Telegram ID участника.")
	}
	return strconv.FormatInt(id, 10), nil
}

func normalizeFilter(f model.Filter) (model.Filter, error) {
	if f.UserID == "" {
		return f, nil
	} // no filter means all owners in the admin API
	var err error
	f.UserID, err = NormalizeUserID(f.UserID)
	return f, err
}
