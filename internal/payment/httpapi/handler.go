package httpapi

import (
	"finote/internal/payment/model"
	"finote/internal/payment/service"
	"net/http"
)

type Handler struct {
	service *service.Service
	mux     *http.ServeMux
}

func New(service *service.Service) *Handler {
	h := &Handler{service: service, mux: http.NewServeMux()}
	h.mux.HandleFunc("POST /v1/payments", h.createPayment)
	h.mux.HandleFunc("GET /v1/payments", h.getPayment)
	h.mux.HandleFunc("PUT /v1/payments/{id}", h.updatePayment)
	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mux.ServeHTTP(w, r)
}

func (h *Handler) createPayment(w http.ResponseWriter, r *http.Request) {
	var body model.Payment
	if err := decodeJSON(w, r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	payment, err := h.service.CreatePayment(r.Context(), body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, payment)
}

func (h *Handler) getPayment(w http.ResponseWriter, r *http.Request) {
	var body model.Payment
	if err := decodeJSON(w, r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	payment, err := h.service.GetPayment(r.Context(), body.ID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, payment)
}

func (h *Handler) updatePayment(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body model.Payment
	if err := decodeJSON(w, r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	payment, err := h.service.UpdatePayment(r.Context(), id, body)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, payment)
}
