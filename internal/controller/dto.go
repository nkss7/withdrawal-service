package controller

import (
	"time"

	"github.com/google/uuid"
	"github.com/nikitafox/withdrawal-service/internal/domain"
)

type CreateWithdrawalRequest struct {
	UserID         uuid.UUID `json:"user_id"`
	Amount         string    `json:"amount"`
	Currency       string    `json:"currency"`
	Destination    string    `json:"destination"`
	IdempotencyKey string    `json:"idempotency_key"`
}

type WithdrawalResponse struct {
	ID             uuid.UUID `json:"id"`
	UserID         uuid.UUID `json:"user_id"`
	Amount         string    `json:"amount"`
	Currency       string    `json:"currency"`
	Destination    string    `json:"destination"`
	IdempotencyKey string    `json:"idempotency_key"`
	Status         string    `json:"status"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

func NewWithdrawalResponse(w *domain.Withdrawal) *WithdrawalResponse {
	return &WithdrawalResponse{
		ID:             w.ID,
		UserID:         w.UserID,
		Amount:         w.Amount,
		Currency:       w.Currency,
		Destination:    w.Destination,
		IdempotencyKey: w.IdempotencyKey,
		Status:         string(w.Status),
		CreatedAt:      w.CreatedAt,
		UpdatedAt:      w.UpdatedAt,
	}
}

type apiError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}
