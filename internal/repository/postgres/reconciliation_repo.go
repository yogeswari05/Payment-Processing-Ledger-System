package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/payflow/payflow/internal/domain"
	"github.com/payflow/payflow/internal/repository"
)

type ReconciliationRepo struct{}

func NewReconciliationRepo() *ReconciliationRepo {
	return &ReconciliationRepo{}
}

func (r *ReconciliationRepo) CreateRun(ctx context.Context, tx repository.DBTX, run *domain.ReconciliationRun) error {
	query := `
		INSERT INTO reconciliation_runs (id, status, started_at, completed_at, total_checks, total_anomalies, summary)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`
	_, err := tx.Exec(
		ctx, query,
		run.ID, run.Status, run.StartedAt, run.CompletedAt,
		run.TotalChecks, run.TotalAnomalies, run.Summary,
	)
	if err != nil {
		return fmt.Errorf("failed to insert reconciliation run: %w", err)
	}
	return nil
}

func (r *ReconciliationRepo) UpdateRun(ctx context.Context, tx repository.DBTX, run *domain.ReconciliationRun) error {
	query := `
		UPDATE reconciliation_runs
		SET
			status = $1,
			completed_at = $2,
			total_checks = $3,
			total_anomalies = $4,
			summary = $5
		WHERE id = $6
	`
	_, err := tx.Exec(
		ctx, query,
		run.Status, run.CompletedAt, run.TotalChecks, run.TotalAnomalies, run.Summary, run.ID,
	)
	if err != nil {
		return fmt.Errorf("failed to update reconciliation run: %w", err)
	}
	return nil
}

func (r *ReconciliationRepo) GetRunByID(ctx context.Context, tx repository.DBTX, id uuid.UUID) (*domain.ReconciliationRun, error) {
	query := `
		SELECT id, status, started_at, completed_at, total_checks, total_anomalies, summary
		FROM reconciliation_runs
		WHERE id = $1
	`
	run := &domain.ReconciliationRun{}
	err := tx.QueryRow(ctx, query, id).Scan(
		&run.ID, &run.Status, &run.StartedAt, &run.CompletedAt,
		&run.TotalChecks, &run.TotalAnomalies, &run.Summary,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrReconciliationNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get reconciliation run: %w", err)
	}

	findingsQuery := `
		SELECT id, run_id, anomaly_type, severity, entity_type, entity_id, description, details, detected_at
		FROM reconciliation_findings
		WHERE run_id = $1
		ORDER BY detected_at ASC
	`
	rows, err := tx.Query(ctx, findingsQuery, id)
	if err != nil {
		return nil, fmt.Errorf("failed to query reconciliation findings: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var f domain.ReconciliationFinding
		if err := rows.Scan(
			&f.ID, &f.RunID, &f.AnomalyType, &f.Severity, &f.EntityType,
			&f.EntityID, &f.Description, &f.Details, &f.DetectedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan finding: %w", err)
		}
		run.Findings = append(run.Findings, f)
	}

	return run, nil
}

func (r *ReconciliationRepo) CreateFindings(ctx context.Context, tx repository.DBTX, findings []domain.ReconciliationFinding) error {
	for _, f := range findings {
		query := `
			INSERT INTO reconciliation_findings (
				id, run_id, anomaly_type, severity, entity_type, entity_id,
				description, details, detected_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		`
		_, err := tx.Exec(
			ctx, query,
			f.ID, f.RunID, f.AnomalyType, f.Severity, f.EntityType, f.EntityID,
			f.Description, f.Details, f.DetectedAt,
		)
		if err != nil {
			return fmt.Errorf("failed to insert finding: %w", err)
		}
	}
	return nil
}

func (r *ReconciliationRepo) CheckUnbalancedTransactions(ctx context.Context, tx repository.DBTX) ([]domain.ReconciliationFinding, error) {
	query := `
		SELECT
			lt.id,
			lt.currency,
			COALESCE(SUM(CASE WHEN le.direction = 'DEBIT' THEN le.amount ELSE 0 END), 0) AS total_debit,
			COALESCE(SUM(CASE WHEN le.direction = 'CREDIT' THEN le.amount ELSE 0 END), 0) AS total_credit
		FROM ledger_transactions lt
		LEFT JOIN ledger_entries le ON le.ledger_transaction_id = lt.id
		GROUP BY lt.id, lt.currency
		HAVING COALESCE(SUM(CASE WHEN le.direction = 'DEBIT' THEN le.amount ELSE 0 END), 0) !=
		       COALESCE(SUM(CASE WHEN le.direction = 'CREDIT' THEN le.amount ELSE 0 END), 0)
	`
	rows, err := tx.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed checking unbalanced transactions: %w", err)
	}
	defer rows.Close()

	var findings []domain.ReconciliationFinding
	for rows.Next() {
		var txID uuid.UUID
		var currency string
		var debit, credit int64
		if err := rows.Scan(&txID, &currency, &debit, &credit); err != nil {
			return nil, err
		}

		details, _ := json.Marshal(map[string]any{
			"currency":     currency,
			"total_debit":  debit,
			"total_credit": credit,
			"difference":   debit - credit,
		})

		findings = append(findings, domain.ReconciliationFinding{
			ID:          uuid.New(),
			AnomalyType: domain.AnomalyUnbalancedLedgerTransaction,
			Severity:    domain.SeverityCritical,
			EntityType:  "ledger_transaction",
			EntityID:    txID,
			Description: fmt.Sprintf("Ledger transaction %s is unbalanced: debits (%d) != credits (%d)", txID, debit, credit),
			Details:     details,
			DetectedAt:  time.Now(),
		})
	}
	return findings, nil
}

func (r *ReconciliationRepo) CheckMissingCapturePostings(ctx context.Context, tx repository.DBTX) ([]domain.ReconciliationFinding, error) {
	query := `
		SELECT p.id, p.status, p.captured_amount, p.currency
		FROM payments p
		LEFT JOIN ledger_transactions lt
			ON lt.business_reference_type = 'PAYMENT_CAPTURE' AND lt.business_reference_id = p.id
		WHERE p.status IN ('captured', 'partially_refunded', 'refunded')
			AND lt.id IS NULL
	`
	rows, err := tx.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed checking missing capture postings: %w", err)
	}
	defer rows.Close()

	var findings []domain.ReconciliationFinding
	for rows.Next() {
		var pID uuid.UUID
		var status string
		var capAmount int64
		var currency string
		if err := rows.Scan(&pID, &status, &capAmount, &currency); err != nil {
			return nil, err
		}

		details, _ := json.Marshal(map[string]any{
			"status":          status,
			"captured_amount": capAmount,
			"currency":        currency,
		})

		findings = append(findings, domain.ReconciliationFinding{
			ID:          uuid.New(),
			AnomalyType: domain.AnomalyMissingCapturePosting,
			Severity:    domain.SeverityCritical,
			EntityType:  "payment",
			EntityID:    pID,
			Description: fmt.Sprintf("Payment %s has captured status '%s' but lacks a PAYMENT_CAPTURE ledger posting", pID, status),
			Details:     details,
			DetectedAt:  time.Now(),
		})
	}
	return findings, nil
}

func (r *ReconciliationRepo) CheckDuplicateCapturePostings(ctx context.Context, tx repository.DBTX) ([]domain.ReconciliationFinding, error) {
	query := `
		SELECT business_reference_id, COUNT(*) AS cnt
		FROM ledger_transactions
		WHERE business_reference_type = 'PAYMENT_CAPTURE'
		GROUP BY business_reference_id
		HAVING COUNT(*) > 1
	`
	rows, err := tx.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed checking duplicate capture postings: %w", err)
	}
	defer rows.Close()

	var findings []domain.ReconciliationFinding
	for rows.Next() {
		var pID uuid.UUID
		var count int
		if err := rows.Scan(&pID, &count); err != nil {
			return nil, err
		}

		details, _ := json.Marshal(map[string]any{"posting_count": count})

		findings = append(findings, domain.ReconciliationFinding{
			ID:          uuid.New(),
			AnomalyType: domain.AnomalyDuplicateCapturePosting,
			Severity:    domain.SeverityCritical,
			EntityType:  "payment",
			EntityID:    pID,
			Description: fmt.Sprintf("Payment %s has %d duplicate PAYMENT_CAPTURE ledger postings", pID, count),
			Details:     details,
			DetectedAt:  time.Now(),
		})
	}
	return findings, nil
}

func (r *ReconciliationRepo) CheckMissingRefundPostings(ctx context.Context, tx repository.DBTX) ([]domain.ReconciliationFinding, error) {
	query := `
		SELECT r.id, r.payment_id, r.amount, r.currency
		FROM refunds r
		LEFT JOIN ledger_transactions lt
			ON lt.business_reference_type = 'PAYMENT_REFUND' AND lt.business_reference_id = r.id
		WHERE r.status = 'succeeded'
			AND lt.id IS NULL
	`
	rows, err := tx.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed checking missing refund postings: %w", err)
	}
	defer rows.Close()

	var findings []domain.ReconciliationFinding
	for rows.Next() {
		var rID, pID uuid.UUID
		var amount int64
		var currency string
		if err := rows.Scan(&rID, &pID, &amount, &currency); err != nil {
			return nil, err
		}

		details, _ := json.Marshal(map[string]any{
			"payment_id": pID,
			"amount":     amount,
			"currency":   currency,
		})

		findings = append(findings, domain.ReconciliationFinding{
			ID:          uuid.New(),
			AnomalyType: domain.AnomalyMissingRefundPosting,
			Severity:    domain.SeverityCritical,
			EntityType:  "refund",
			EntityID:    rID,
			Description: fmt.Sprintf("Refund %s succeeded but lacks a PAYMENT_REFUND ledger posting", rID),
			Details:     details,
			DetectedAt:  time.Now(),
		})
	}
	return findings, nil
}

func (r *ReconciliationRepo) CheckRefundExceedsCapture(ctx context.Context, tx repository.DBTX) ([]domain.ReconciliationFinding, error) {
	query := `
		SELECT
			p.id,
			p.captured_amount,
			p.refunded_amount,
			COALESCE(SUM(r.amount), 0) AS total_refunds_sum
		FROM payments p
		LEFT JOIN refunds r ON r.payment_id = p.id AND r.status = 'succeeded'
		GROUP BY p.id, p.captured_amount, p.refunded_amount
		HAVING p.refunded_amount > p.captured_amount
		    OR p.refunded_amount != COALESCE(SUM(r.amount), 0)
	`
	rows, err := tx.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed checking refund totals: %w", err)
	}
	defer rows.Close()

	var findings []domain.ReconciliationFinding
	for rows.Next() {
		var pID uuid.UUID
		var capAmount, refAmount, sumAmount int64
		if err := rows.Scan(&pID, &capAmount, &refAmount, &sumAmount); err != nil {
			return nil, err
		}

		details, _ := json.Marshal(map[string]any{
			"captured_amount":   capAmount,
			"refunded_amount":   refAmount,
			"total_refunds_sum": sumAmount,
		})

		findings = append(findings, domain.ReconciliationFinding{
			ID:          uuid.New(),
			AnomalyType: domain.AnomalyRefundExceedsCapture,
			Severity:    domain.SeverityCritical,
			EntityType:  "payment",
			EntityID:    pID,
			Description: fmt.Sprintf("Payment %s has inconsistent refund totals (captured: %d, recorded refunded: %d, sum of refunds: %d)", pID, capAmount, refAmount, sumAmount),
			Details:     details,
			DetectedAt:  time.Now(),
		})
	}
	return findings, nil
}

func (r *ReconciliationRepo) CheckPaymentStatusMismatches(ctx context.Context, tx repository.DBTX) ([]domain.ReconciliationFinding, error) {
	query := `
		SELECT id, status, amount, captured_amount, refunded_amount
		FROM payments
		WHERE (status IN ('captured', 'partially_refunded') AND captured_amount <= 0)
		   OR (status = 'refunded' AND (captured_amount <= 0 OR refunded_amount != captured_amount))
		   OR (status IN ('created', 'authorized', 'canceled', 'failed') AND captured_amount > 0)
	`
	rows, err := tx.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed checking payment status mismatches: %w", err)
	}
	defer rows.Close()

	var findings []domain.ReconciliationFinding
	for rows.Next() {
		var pID uuid.UUID
		var status string
		var amount, capAmount, refAmount int64
		if err := rows.Scan(&pID, &status, &amount, &capAmount, &refAmount); err != nil {
			return nil, err
		}

		details, _ := json.Marshal(map[string]any{
			"status":          status,
			"amount":          amount,
			"captured_amount": capAmount,
			"refunded_amount": refAmount,
		})

		findings = append(findings, domain.ReconciliationFinding{
			ID:          uuid.New(),
			AnomalyType: domain.AnomalyPaymentStatusMismatch,
			Severity:    domain.SeverityHigh,
			EntityType:  "payment",
			EntityID:    pID,
			Description: fmt.Sprintf("Payment %s has status '%s' incompatible with captured (%d) and refunded (%d) amounts", pID, status, capAmount, refAmount),
			Details:     details,
			DetectedAt:  time.Now(),
		})
	}
	return findings, nil
}

func (r *ReconciliationRepo) CheckOrphanedLedgerTransactions(ctx context.Context, tx repository.DBTX) ([]domain.ReconciliationFinding, error) {
	query := `
		SELECT lt.id, lt.business_reference_type, lt.business_reference_id
		FROM ledger_transactions lt
		LEFT JOIN payments p ON lt.business_reference_type = 'PAYMENT_CAPTURE' AND lt.business_reference_id = p.id
		LEFT JOIN refunds r ON lt.business_reference_type = 'PAYMENT_REFUND' AND lt.business_reference_id = r.id
		WHERE (lt.business_reference_type = 'PAYMENT_CAPTURE' AND p.id IS NULL)
		   OR (lt.business_reference_type = 'PAYMENT_REFUND' AND r.id IS NULL)
	`
	rows, err := tx.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed checking orphaned ledger transactions: %w", err)
	}
	defer rows.Close()

	var findings []domain.ReconciliationFinding
	for rows.Next() {
		var txID, refID uuid.UUID
		var refType string
		if err := rows.Scan(&txID, &refType, &refID); err != nil {
			return nil, err
		}

		details, _ := json.Marshal(map[string]any{
			"business_reference_type": refType,
			"business_reference_id":   refID,
		})

		findings = append(findings, domain.ReconciliationFinding{
			ID:          uuid.New(),
			AnomalyType: domain.AnomalyOrphanedLedgerTransaction,
			Severity:    domain.SeverityHigh,
			EntityType:  "ledger_transaction",
			EntityID:    txID,
			Description: fmt.Sprintf("Ledger transaction %s references non-existent %s %s", txID, refType, refID),
			Details:     details,
			DetectedAt:  time.Now(),
		})
	}
	return findings, nil
}
