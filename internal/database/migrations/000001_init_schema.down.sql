-- Schema Migration 000001 Down: Drop PayFlow Database Schema

DROP TABLE IF EXISTS reconciliation_findings CASCADE;
DROP TABLE IF EXISTS reconciliation_runs CASCADE;
DROP TABLE IF EXISTS consumed_events CASCADE;
DROP TABLE IF EXISTS outbox_events CASCADE;
DROP TABLE IF EXISTS idempotency_keys CASCADE;
DROP TRIGGER IF EXISTS trg_immutable_ledger_entries ON ledger_entries;
DROP TRIGGER IF EXISTS trg_immutable_ledger_transactions ON ledger_transactions;
DROP FUNCTION IF EXISTS forbid_ledger_modification CASCADE;
DROP TABLE IF EXISTS ledger_entries CASCADE;
DROP TABLE IF EXISTS ledger_transactions CASCADE;
DROP TABLE IF EXISTS ledger_accounts CASCADE;
DROP TABLE IF EXISTS refunds CASCADE;
DROP TABLE IF EXISTS payments CASCADE;
DROP TABLE IF EXISTS customers CASCADE;
DROP TABLE IF EXISTS merchants CASCADE;
