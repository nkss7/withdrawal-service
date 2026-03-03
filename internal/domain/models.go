package domain

import (
	"time"

	"github.com/google/uuid"
)

type WithdrawalStatus string

const (
	StatusPending   WithdrawalStatus = "pending"
	StatusConfirmed WithdrawalStatus = "confirmed"
	StatusFailed    WithdrawalStatus = "failed"
)

type Withdrawal struct {
	ID             uuid.UUID
	UserID         uuid.UUID
	Amount         string
	Currency       string
	Destination    string
	IdempotencyKey string
	Status         WithdrawalStatus
	CreatedAt      time.Time
	UpdatedAt      time.Time
}
