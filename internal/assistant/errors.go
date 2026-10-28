package assistant

import (
	"context"
	"errors"
	"fmt"
	"net/http"
)

var ErrPlannerUnavailable = errors.New("LLM service unavailable")

type PlannerHTTPError struct{ Code int }

func (e PlannerHTTPError) Error() string { return fmt.Sprintf("LLM HTTP %d", e.Code) }
func (e PlannerHTTPError) Unwrap() error {
	if e.Code == http.StatusServiceUnavailable {
		return ErrPlannerUnavailable
	}
	return nil
}

func plannerErrorReply(err error) string {
	var status PlannerHTTPError
	if errors.As(err, &status) {
		switch {
		case status.Code == 401 || status.Code == 403 || status.Code == 404:
			return "ИИ-сервис недоступен из-за настроек подключения. Расход не сохранён. Сообщите администратору."
		case status.Code == 429:
			return "Достигнут лимит запросов к ИИ-сервису. Расход не сохранён. Попробуйте позже."
		case status.Code >= 500:
			return "ИИ-сервис временно недоступен. Расход не сохранён. Попробуйте позже"
		}
	}
	if errors.Is(err, ErrPlannerUnavailable) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return "ИИ-сервис временно недоступен. Расход не сохранён. Попробуйте позже"
	}
	return "Не удалось разобрать запрос. Укажите действие, сумму и описание; если ошибка повторяется, попробуйте позже."
}
