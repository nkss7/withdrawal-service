-- Withdrawal Service Schema
-- PostgreSQL 14+

BEGIN;

-- Таблица балансов пользователей
CREATE TABLE IF NOT EXISTS balances (
    user_id     UUID PRIMARY KEY,
    amount      DECIMAL(20, 8) NOT NULL DEFAULT 0 CHECK (amount >= 0),
    currency    VARCHAR(10) NOT NULL DEFAULT 'USDT',
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Таблица заявок на вывод
CREATE TABLE IF NOT EXISTS withdrawals (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id          UUID NOT NULL REFERENCES balances(user_id),
    amount           DECIMAL(20, 8) NOT NULL CHECK (amount > 0),
    currency         VARCHAR(10) NOT NULL DEFAULT 'USDT',
    destination      TEXT NOT NULL,
    idempotency_key  VARCHAR(255) NOT NULL,
    status           VARCHAR(20) NOT NULL DEFAULT 'pending',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT withdrawals_idempotency_key_unique UNIQUE (idempotency_key)
);

-- Таблица записей в ledger (optional)
CREATE TABLE IF NOT EXISTS ledger_entries (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    withdrawal_id   UUID NOT NULL REFERENCES withdrawals(id),
    user_id         UUID NOT NULL,
    entry_type      VARCHAR(20) NOT NULL, -- 'debit', 'credit'
    amount          DECIMAL(20, 8) NOT NULL,
    balance_after   DECIMAL(20, 8) NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Индексы
CREATE INDEX IF NOT EXISTS idx_withdrawals_user_id ON withdrawals(user_id);
CREATE INDEX IF NOT EXISTS idx_withdrawals_status ON withdrawals(status);
CREATE INDEX IF NOT EXISTS idx_ledger_entries_withdrawal_id ON ledger_entries(withdrawal_id);
CREATE INDEX IF NOT EXISTS idx_ledger_entries_user_id ON ledger_entries(user_id);

-- Тестовый пользователь с начальным балансом 1000 USDT
INSERT INTO balances (user_id, amount, currency)
VALUES ('00000000-0000-0000-0000-000000000001', 1000.00000000, 'USDT')
ON CONFLICT (user_id) DO NOTHING;

COMMIT;
