package httpapi

import (
	"finote/internal/payment/model"
	"finote/internal/payment/service"
	"finote/internal/web"
	"net/http"
)

type Handler struct {
	service *service.Service
	mux     *http.ServeMux
}

func New(service *service.Service) *Handler {
	h := &Handler{service: service, mux: http.NewServeMux()}
	h.mux.HandleFunc("POST /v1/payments", h.createPayment)
	h.mux.HandleFunc("GET /v1/payments", h.listPayments)
	h.mux.HandleFunc("GET /v1/payments/{id}", h.getPayment)
	h.mux.HandleFunc("GET /v1/users", h.listUsers)
	h.mux.HandleFunc("PATCH /v1/payments/{id}", h.patchPayment)
	h.mux.HandleFunc("PUT /v1/payments/{id}", h.updatePayment)
	h.mux.HandleFunc("DELETE /v1/payments/{id}", h.deletePayment)
	h.mux.Handle("GET /", web.Handler())
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
		respondError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, payment)
}

func (h *Handler) getPayment(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validID(w, id) {
		return
	}
	payment, err := h.service.GetPayment(r.Context(), id)
	if err != nil {
		respondError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, payment)
}

func (h *Handler) updatePayment(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validID(w, id) {
		return
	}
	var body model.Payment
	if err := decodeJSON(w, r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	payment, err := h.service.UpdatePayment(r.Context(), id, body)
	if err != nil {
		respondError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, payment)
}

func (h *Handler) deletePayment(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validID(w, id) {
		return
	}
	err := h.service.DeletePayment(r.Context(), id)
	if err != nil {
		respondError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
