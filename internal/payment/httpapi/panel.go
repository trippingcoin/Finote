package httpapi

import (
	"database/sql"
	"encoding/hex"
	"errors"
	"finote/internal/payment/model"
	"finote/internal/payment/service"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"
)

func respondError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrValidation):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": strings.TrimPrefix(err.Error(), service.ErrValidation.Error()+": ")})
	case errors.Is(err, sql.ErrNoRows):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Расход не найден. Возможно, он уже удалён."})
	default:
		slog.Error("payment API request failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Не удалось выполнить запрос. Попробуйте ещё раз."})
	}
}

func validID(w http.ResponseWriter, id string) bool {
	if len(id) == 36 && id[8] == '-' && id[13] == '-' && id[18] == '-' && id[23] == '-' {
		if b, err := hex.DecodeString(strings.ReplaceAll(id, "-", "")); err == nil && len(b) == 16 {
			return true
		}
	}
	writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Некорректный ID расхода."})
	return false
}

func (h *Handler) listPayments(w http.ResponseWriter, r *http.Request) {
	f, err := parseFilter(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	result, err := h.service.ListPayments(r.Context(), f)
	if err != nil {
		respondError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func parseFilter(r *http.Request) (model.Filter, error) {
	q := r.URL.Query()
	f := model.Filter{UserID: strings.TrimSpace(q.Get("user_id")), Category: q.Get("category"), Limit: 20}
	bad := errors.New("Проверьте фильтры: даты, категорию и параметры страницы.")
	if f.Category != "" && !service.ValidateCategory(f.Category) {
		return f, bad
	}
	// The UI uses inclusive calendar dates in the same timezone as the bot by default.
	timezone := q.Get("timezone")
	if timezone == "" {
		timezone = "Asia/Almaty"
	}
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return f, bad
	}
	for _, key := range []string{"from", "to"} {
		if value := q.Get(key); value != "" {
			date, err := time.ParseInLocation("2006-01-02", value, loc)
			if err != nil {
				return f, bad
			}
			if key == "from" {
				f.From = &date
			} else {
				end := date.AddDate(0, 0, 1)
				f.To = &end
			}
		}
	}
	if f.From != nil && f.To != nil && !f.From.Before(*f.To) {
		return f, bad
	}
	if v := q.Get("limit"); v != "" {
		f.Limit, err = strconv.Atoi(v)
		if err != nil || f.Limit < 1 || f.Limit > 100 {
			return f, bad
		}
	}
	if v := q.Get("offset"); v != "" {
		f.Offset, err = strconv.Atoi(v)
		if err != nil || f.Offset < 0 || f.Offset > 10000000 {
			return f, bad
		}
	}
	return f, nil
}

func (h *Handler) listUsers(w http.ResponseWriter, r *http.Request) {
	users, err := h.service.ListUsers(r.Context())
	if err != nil {
		respondError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, users)
}

func (h *Handler) patchPayment(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validID(w, id) {
		return
	}
	var body model.UpdatePayment
	if err := decodeJSON(w, r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Проверьте формат полей запроса."})
		return
	}
	p, err := h.service.PatchPayment(r.Context(), id, body)
	if err != nil {
		respondError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}
