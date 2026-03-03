package controller

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/nikitafox/withdrawal-service/internal/domain"
	"github.com/nikitafox/withdrawal-service/internal/service"
)

const (
	errInvalidRequestBody  = "invalid request body"
	errInvalidWithdrawalID = "invalid withdrawal id"
	errInternalServer      = "internal server error"
)

type WithdrawalController struct {
	svc    *service.WithdrawalService
	logger *slog.Logger
}

func NewWithdrawalController(svc *service.WithdrawalService, logger *slog.Logger) *WithdrawalController {
	return &WithdrawalController{
		svc:    svc,
		logger: logger,
	}
}

func (c *WithdrawalController) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/withdrawals", c.CreateWithdrawal)
	mux.HandleFunc("GET /v1/withdrawals/{id}", c.GetWithdrawal)
	mux.HandleFunc("POST /v1/withdrawals/{id}/confirm", c.ConfirmWithdrawal)
}

func (c *WithdrawalController) CreateWithdrawal(w http.ResponseWriter, r *http.Request) {
	var req CreateWithdrawalRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		c.writeError(w, http.StatusBadRequest, errInvalidRequestBody)
		return
	}

	req.Currency = strings.ToUpper(strings.TrimSpace(req.Currency))

	withdrawal, err := c.svc.Create(r.Context(), req.UserID, req.Amount, req.Currency, req.Destination, req.IdempotencyKey)
	if err != nil {
		c.handleServiceError(w, r, err)
		return
	}

	c.writeJSON(w, http.StatusCreated, NewWithdrawalResponse(withdrawal))
}

func (c *WithdrawalController) GetWithdrawal(w http.ResponseWriter, r *http.Request) {
	id, ok := c.parsePathID(w, r)
	if !ok {
		return
	}

	withdrawal, err := c.svc.GetByID(r.Context(), id)
	if err != nil {
		c.handleServiceError(w, r, err)
		return
	}

	c.writeJSON(w, http.StatusOK, NewWithdrawalResponse(withdrawal))
}

func (c *WithdrawalController) ConfirmWithdrawal(w http.ResponseWriter, r *http.Request) {
	id, ok := c.parsePathID(w, r)
	if !ok {
		return
	}

	withdrawal, err := c.svc.Confirm(r.Context(), id)
	if err != nil {
		c.handleServiceError(w, r, err)
		return
	}

	c.writeJSON(w, http.StatusOK, NewWithdrawalResponse(withdrawal))
}

func (c *WithdrawalController) parsePathID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		c.writeError(w, http.StatusBadRequest, errInvalidWithdrawalID)
		return uuid.Nil, false
	}
	return id, true
}

func (c *WithdrawalController) handleServiceError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, domain.ErrInvalidInput):
		c.writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, domain.ErrInsufficientBalance):
		c.writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, domain.ErrIdempotencyConflict):
		c.writeError(w, http.StatusUnprocessableEntity, err.Error())
	case errors.Is(err, domain.ErrWithdrawalNotFound):
		c.writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, domain.ErrUserNotFound):
		c.writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, domain.ErrInvalidStatus):
		c.writeError(w, http.StatusConflict, err.Error())
	default:
		c.logger.ErrorContext(r.Context(), "internal error", slog.String("error", err.Error()))
		c.writeError(w, http.StatusInternalServerError, errInternalServer)
	}
}

func (c *WithdrawalController) writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		c.logger.Error("failed to encode response", slog.String("error", err.Error()))
	}
}

func (c *WithdrawalController) writeError(w http.ResponseWriter, status int, message string) {
	c.writeJSON(w, status, apiError{
		Code:    status,
		Message: message,
	})
}
