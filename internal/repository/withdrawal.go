package repository

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/google/uuid"
	"github.com/nikitafox/withdrawal-service/internal/domain"
)

var withdrawalColumns = `id, user_id, amount, currency, destination, idempotency_key, status, created_at, updated_at`

func scanWithdrawal(row interface{ Scan(dest ...any) error }) (*domain.Withdrawal, error) {
	w := &domain.Withdrawal{}
	err := row.Scan(&w.ID, &w.UserID, &w.Amount, &w.Currency, &w.Destination,
		&w.IdempotencyKey, &w.Status, &w.CreatedAt, &w.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return w, nil
}

type WithdrawalRepository interface {
	CreateWithdrawal(ctx context.Context, userID uuid.UUID, amount, currency, destination, idempotencyKey string) (*domain.Withdrawal, error)
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Withdrawal, error)
	GetByIdempotencyKey(ctx context.Context, key string) (*domain.Withdrawal, error)
	ConfirmWithdrawal(ctx context.Context, id uuid.UUID) (*domain.Withdrawal, error)
}

type postgresRepo struct {
	db *sql.DB
}

func NewWithdrawalRepository(db *sql.DB) WithdrawalRepository {
	return &postgresRepo{db: db}
}

func (r *postgresRepo) CreateWithdrawal(ctx context.Context, userID uuid.UUID, amount, currency, destination, idempotencyKey string) (*domain.Withdrawal, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	var currentBalance string
	err = tx.QueryRowContext(ctx,
		`SELECT amount FROM balances WHERE user_id = $1 FOR UPDATE`,
		userID,
	).Scan(&currentBalance)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, domain.ErrUserNotFound
		}
		return nil, fmt.Errorf("lock balance: %w", err)
	}

	var hasSufficientBalance bool
	err = tx.QueryRowContext(ctx,
		`SELECT $1::DECIMAL(20,8) <= $2::DECIMAL(20,8)`,
		amount, currentBalance,
	).Scan(&hasSufficientBalance)
	if err != nil {
		return nil, fmt.Errorf("check balance: %w", err)
	}
	if !hasSufficientBalance {
		return nil, domain.ErrInsufficientBalance
	}

	var newBalance string
	err = tx.QueryRowContext(ctx,
		`UPDATE balances
		 SET amount = amount - $1::DECIMAL(20,8), updated_at = NOW()
		 WHERE user_id = $2
		 RETURNING amount`,
		amount, userID,
	).Scan(&newBalance)
	if err != nil {
		return nil, fmt.Errorf("update balance: %w", err)
	}

	w, err := scanWithdrawal(tx.QueryRowContext(ctx,
		`INSERT INTO withdrawals (user_id, amount, currency, destination, idempotency_key, status)
		 VALUES ($1, $2::DECIMAL(20,8), $3, $4, $5, $6)
		 RETURNING `+withdrawalColumns,
		userID, amount, currency, destination, idempotencyKey, domain.StatusPending,
	))
	if err != nil {
		return nil, fmt.Errorf("insert withdrawal: %w", err)
	}

	_, err = tx.ExecContext(ctx,
		`INSERT INTO ledger_entries (withdrawal_id, user_id, entry_type, amount, balance_after)
		 VALUES ($1, $2, 'debit', $3::DECIMAL(20,8), $4::DECIMAL(20,8))`,
		w.ID, userID, amount, newBalance,
	)
	if err != nil {
		return nil, fmt.Errorf("insert ledger entry: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit transaction: %w", err)
	}

	return w, nil
}

func (r *postgresRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.Withdrawal, error) {
	w, err := scanWithdrawal(r.db.QueryRowContext(ctx,
		`SELECT `+withdrawalColumns+` FROM withdrawals WHERE id = $1`,
		id,
	))
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, domain.ErrWithdrawalNotFound
		}
		return nil, fmt.Errorf("get withdrawal by id: %w", err)
	}
	return w, nil
}

func (r *postgresRepo) GetByIdempotencyKey(ctx context.Context, key string) (*domain.Withdrawal, error) {
	w, err := scanWithdrawal(r.db.QueryRowContext(ctx,
		`SELECT `+withdrawalColumns+` FROM withdrawals WHERE idempotency_key = $1`,
		key,
	))
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get withdrawal by idempotency key: %w", err)
	}
	return w, nil
}

func (r *postgresRepo) ConfirmWithdrawal(ctx context.Context, id uuid.UUID) (*domain.Withdrawal, error) {
	w, err := scanWithdrawal(r.db.QueryRowContext(ctx,
		`UPDATE withdrawals
		 SET status = $1, updated_at = NOW()
		 WHERE id = $2 AND status = $3
		 RETURNING `+withdrawalColumns,
		domain.StatusConfirmed, id, domain.StatusPending,
	))
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, domain.ErrInvalidStatus
		}
		return nil, fmt.Errorf("confirm withdrawal: %w", err)
	}
	return w, nil
}
