package domain

import "errors"

var (
	ErrInsufficientBalance = errors.New("insufficient balance")
	ErrIdempotencyConflict = errors.New("idempotency key conflict: payload mismatch")
	ErrWithdrawalNotFound  = errors.New("withdrawal not found")
	ErrInvalidInput        = errors.New("invalid input")
	ErrUserNotFound        = errors.New("user not found")
	ErrInvalidStatus       = errors.New("invalid withdrawal status for this operation")
)
