-- Schema Migration 000001: Initial PayFlow Database Schema
-- Supports merchants, customers, payments, refunds, double-entry ledger,
-- idempotency, transactional outbox, and reconciliation.

CREATE EXTENSION IF NOT EXISTS "pgcrypto";

-- Merchants
CREATE TABLE merchants (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(255) NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'suspended', 'closed')),
    api_key_hash VARCHAR(64) NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Customers
CREATE TABLE customers (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    merchant_id UUID NOT NULL REFERENCES merchants(id) ON DELETE RESTRICT,
    email VARCHAR(255) NOT NULL,
    name VARCHAR(255) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_merchant_customer_email UNIQUE (merchant_id, email)
);

-- Payments
CREATE TABLE payments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    merchant_id UUID NOT NULL REFERENCES merchants(id) ON DELETE RESTRICT,
    customer_id UUID NOT NULL REFERENCES customers(id) ON DELETE RESTRICT,
    amount BIGINT NOT NULL CHECK (amount > 0),
    currency VARCHAR(3) NOT NULL CHECK (currency IN ('USD', 'EUR', 'GBP')),
    status VARCHAR(32) NOT NULL CHECK (status IN ('created', 'authorized', 'captured', 'partially_refunded', 'refunded', 'canceled', 'failed')),
    captured_amount BIGINT NOT NULL DEFAULT 0 CHECK (captured_amount >= 0 AND captured_amount <= amount),
    refunded_amount BIGINT NOT NULL DEFAULT 0 CHECK (refunded_amount >= 0 AND refunded_amount <= captured_amount),
    description VARCHAR(500) NOT NULL DEFAULT '',
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    version INT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_payments_merchant_status ON payments(merchant_id, status);
CREATE INDEX idx_payments_customer ON payments(customer_id);
CREATE INDEX idx_payments_created_at ON payments(created_at);

-- Refunds
CREATE TABLE refunds (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    payment_id UUID NOT NULL REFERENCES payments(id) ON DELETE RESTRICT,
    merchant_id UUID NOT NULL REFERENCES merchants(id) ON DELETE RESTRICT,
    amount BIGINT NOT NULL CHECK (amount > 0),
    currency VARCHAR(3) NOT NULL CHECK (currency IN ('USD', 'EUR', 'GBP')),
    status VARCHAR(32) NOT NULL CHECK (status IN ('pending', 'succeeded', 'failed')),
    reason VARCHAR(255) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_refunds_payment ON refunds(payment_id);
CREATE INDEX idx_refunds_merchant ON refunds(merchant_id);

-- Ledger Accounts
CREATE TABLE ledger_accounts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    account_type VARCHAR(32) NOT NULL CHECK (account_type IN ('asset', 'liability', 'equity', 'revenue', 'expense')),
    name VARCHAR(255) NOT NULL,
    currency VARCHAR(3) NOT NULL CHECK (currency IN ('USD', 'EUR', 'GBP')),
    merchant_id UUID NULL REFERENCES merchants(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_ledger_account_scope UNIQUE (account_type, name, currency, merchant_id)
);

-- Ledger Transactions
CREATE TABLE ledger_transactions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_reference_type VARCHAR(64) NOT NULL CHECK (business_reference_type IN ('PAYMENT_CAPTURE', 'PAYMENT_REFUND', 'ADJUSTMENT')),
    business_reference_id UUID NOT NULL,
    transaction_type VARCHAR(64) NOT NULL,
    currency VARCHAR(3) NOT NULL CHECK (currency IN ('USD', 'EUR', 'GBP')),
    status VARCHAR(32) NOT NULL DEFAULT 'POSTED' CHECK (status IN ('POSTED', 'VOIDED')),
    description VARCHAR(500) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_business_reference UNIQUE (business_reference_type, business_reference_id)
);

-- Ledger Entries
CREATE TABLE ledger_entries (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    ledger_transaction_id UUID NOT NULL REFERENCES ledger_transactions(id) ON DELETE RESTRICT,
    ledger_account_id UUID NOT NULL REFERENCES ledger_accounts(id) ON DELETE RESTRICT,
    direction VARCHAR(6) NOT NULL CHECK (direction IN ('DEBIT', 'CREDIT')),
    amount BIGINT NOT NULL CHECK (amount > 0),
    currency VARCHAR(3) NOT NULL CHECK (currency IN ('USD', 'EUR', 'GBP')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_ledger_entries_account ON ledger_entries(ledger_account_id);
CREATE INDEX idx_ledger_entries_transaction ON ledger_entries(ledger_transaction_id);

-- Prevent update/delete on ledger_entries and ledger_transactions (immutability)
CREATE OR REPLACE FUNCTION forbid_ledger_modification()
RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'Ledger entries and transactions are strictly immutable once created';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_immutable_ledger_entries
    BEFORE UPDATE OR DELETE ON ledger_entries
    FOR EACH ROW EXECUTE FUNCTION forbid_ledger_modification();

CREATE TRIGGER trg_immutable_ledger_transactions
    BEFORE UPDATE OR DELETE ON ledger_transactions
    FOR EACH ROW EXECUTE FUNCTION forbid_ledger_modification();

-- Idempotency Records
CREATE TABLE idempotency_keys (
    merchant_id UUID NOT NULL REFERENCES merchants(id) ON DELETE CASCADE,
    key VARCHAR(128) NOT NULL,
    request_hash VARCHAR(64) NOT NULL,
    status VARCHAR(32) NOT NULL CHECK (status IN ('started', 'completed')),
    resource_id UUID NULL,
    response_code INT NULL,
    response_body JSONB NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (merchant_id, key)
);

CREATE INDEX idx_idempotency_expires_at ON idempotency_keys(expires_at);

-- Outbox Events
CREATE TABLE outbox_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_type VARCHAR(64) NOT NULL,
    aggregate_type VARCHAR(64) NOT NULL,
    aggregate_id UUID NOT NULL,
    payload JSONB NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'PENDING' CHECK (status IN ('PENDING', 'PROCESSING', 'PUBLISHED', 'DEAD_LETTER')),
    attempts INT NOT NULL DEFAULT 0,
    max_attempts INT NOT NULL DEFAULT 5,
    next_retry_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    locked_at TIMESTAMPTZ NULL,
    locked_by VARCHAR(128) NULL,
    last_error TEXT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    published_at TIMESTAMPTZ NULL
);

CREATE INDEX idx_outbox_events_status_retry ON outbox_events(status, next_retry_at)
    WHERE status IN ('PENDING', 'PROCESSING');

-- Consumed Events (for simulating downstream idempotent event processing)
CREATE TABLE consumed_events (
    event_id UUID PRIMARY KEY,
    consumer_name VARCHAR(128) NOT NULL,
    consumed_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Reconciliation Runs & Findings
CREATE TABLE reconciliation_runs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    status VARCHAR(32) NOT NULL CHECK (status IN ('RUNNING', 'COMPLETED', 'FAILED')),
    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ NULL,
    total_checks INT NOT NULL DEFAULT 0,
    total_anomalies INT NOT NULL DEFAULT 0,
    summary JSONB NOT NULL DEFAULT '{}'::jsonb
);

CREATE TABLE reconciliation_findings (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id UUID NOT NULL REFERENCES reconciliation_runs(id) ON DELETE CASCADE,
    anomaly_type VARCHAR(64) NOT NULL,
    severity VARCHAR(32) NOT NULL CHECK (severity IN ('LOW', 'MEDIUM', 'HIGH', 'CRITICAL')),
    entity_type VARCHAR(64) NOT NULL,
    entity_id UUID NOT NULL,
    description TEXT NOT NULL,
    details JSONB NOT NULL DEFAULT '{}'::jsonb,
    detected_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_reconciliation_findings_run ON reconciliation_findings(run_id);
