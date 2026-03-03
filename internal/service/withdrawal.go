package service

import (
	"context"
	"fmt"
	"log/slog"
	"math/big"
	"strings"

	"github.com/google/uuid"
	"github.com/nikitafox/withdrawal-service/internal/domain"
	"github.com/nikitafox/withdrawal-service/internal/locker"
	"github.com/nikitafox/withdrawal-service/internal/repository"
)

type WithdrawalService struct {
	repo   repository.WithdrawalRepository
	locker *locker.UserLocker
	logger *slog.Logger
}

func NewWithdrawalService(repo repository.WithdrawalRepository, locker *locker.UserLocker, logger *slog.Logger) *WithdrawalService {
	return &WithdrawalService{
		repo:   repo,
		locker: locker,
		logger: logger,
	}
}

func (s *WithdrawalService) Create(ctx context.Context, userID uuid.UUID, amount, currency, destination, idempotencyKey string) (*domain.Withdrawal, error) {

	if err := s.validateCreateRequest(userID, amount, currency, destination, idempotencyKey); err != nil {
		return nil, err
	}

	s.locker.Lock(userID)
	defer s.locker.Unlock(userID)

	existing, err := s.repo.GetByIdempotencyKey(ctx, idempotencyKey)
	if err != nil {
		return nil, fmt.Errorf("check idempotency: %w", err)
	}

	if existing != nil {
		if s.payloadMatches(existing, userID, amount, currency, destination) {
			s.logger.InfoContext(ctx, "idempotent request: returning existing withdrawal",
				slog.String("withdrawal_id", existing.ID.String()),
				slog.String("idempotency_key", idempotencyKey),
			)
			return existing, nil
		}

		s.logger.WarnContext(ctx, "idempotency conflict: payload mismatch",
			slog.String("idempotency_key", idempotencyKey),
		)
		return nil, domain.ErrIdempotencyConflict
	}

	w, err := s.repo.CreateWithdrawal(ctx, userID, amount, currency, destination, idempotencyKey)
	if err != nil {
		// Race condition: между проверкой идемпотентности и INSERT могла появиться запись с тем же ключом.
		if strings.Contains(err.Error(), "withdrawals_idempotency_key_unique") ||
			strings.Contains(err.Error(), "duplicate key") {

			existing, retryErr := s.repo.GetByIdempotencyKey(ctx, idempotencyKey)
			if retryErr != nil {
				return nil, fmt.Errorf("retry idempotency check: %w", retryErr)
			}
			if existing != nil && s.payloadMatches(existing, userID, amount, currency, destination) {
				return existing, nil
			}
			return nil, domain.ErrIdempotencyConflict
		}
		s.logger.ErrorContext(ctx, "failed to create withdrawal",
			slog.String("error", err.Error()),
			slog.String("user_id", userID.String()),
		)
		return nil, err
	}

	s.logger.InfoContext(ctx, "withdrawal created",
		slog.String("withdrawal_id", w.ID.String()),
		slog.String("user_id", userID.String()),
		slog.String("amount", amount),
		slog.String("status", string(w.Status)),
	)

	return w, nil
}

func (s *WithdrawalService) GetByID(ctx context.Context, id uuid.UUID) (*domain.Withdrawal, error) {
	w, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return w, nil
}

func (s *WithdrawalService) Confirm(ctx context.Context, id uuid.UUID) (*domain.Withdrawal, error) {
	w, err := s.repo.ConfirmWithdrawal(ctx, id)
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to confirm withdrawal",
			slog.String("error", err.Error()),
			slog.String("withdrawal_id", id.String()),
		)
		return nil, err
	}

	s.logger.InfoContext(ctx, "withdrawal confirmed",
		slog.String("withdrawal_id", w.ID.String()),
		slog.String("user_id", w.UserID.String()),
		slog.String("status", string(w.Status)),
	)

	return w, nil
}

func (s *WithdrawalService) validateCreateRequest(userID uuid.UUID, amount, currency, destination, idempotencyKey string) error {
	if userID == uuid.Nil {
		return fmt.Errorf("%w: user_id is required", domain.ErrInvalidInput)
	}

	if idempotencyKey == "" {
		return fmt.Errorf("%w: idempotency_key is required", domain.ErrInvalidInput)
	}

	if strings.TrimSpace(destination) == "" {
		return fmt.Errorf("%w: destination is required", domain.ErrInvalidInput)
	}

	a, ok := new(big.Float).SetString(amount)
	if !ok {
		return fmt.Errorf("%w: amount must be a valid number", domain.ErrInvalidInput)
	}
	if a.Cmp(big.NewFloat(0)) <= 0 {
		return fmt.Errorf("%w: amount must be greater than 0", domain.ErrInvalidInput)
	}

	if strings.ToUpper(currency) != "USDT" {
		return fmt.Errorf("%w: currency must be USDT", domain.ErrInvalidInput)
	}

	return nil
}

func (s *WithdrawalService) payloadMatches(existing *domain.Withdrawal, userID uuid.UUID, amount, currency, destination string) bool {
	existingAmount, ok1 := new(big.Float).SetString(existing.Amount)
	reqAmount, ok2 := new(big.Float).SetString(amount)
	if !ok1 || !ok2 {
		return false
	}

	return existing.UserID == userID &&
		existingAmount.Cmp(reqAmount) == 0 &&
		strings.EqualFold(existing.Currency, currency) &&
		existing.Destination == destination
}
