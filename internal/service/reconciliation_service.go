package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/payflow/payflow/internal/domain"
	"github.com/payflow/payflow/internal/repository"
)

type ReconciliationService struct {
	txManager repository.TxManager
	reconRepo repository.ReconciliationRepo
	logger    *slog.Logger
}

func NewReconciliationService(
	tm repository.TxManager,
	rr repository.ReconciliationRepo,
	logger *slog.Logger,
) *ReconciliationService {
	return &ReconciliationService{
		txManager: tm,
		reconRepo: rr,
		logger:    logger.With("component", "reconciliation_service"),
	}
}

// RunAudit independently scans all payments, refunds, and ledger records to detect invariant violations.
// All checks are performed within a consistent snapshot transaction.
func (s *ReconciliationService) RunAudit(ctx context.Context) (*domain.ReconciliationRun, error) {
	runID := uuid.New()
	startTime := time.Now()

	run := &domain.ReconciliationRun{
		ID:             runID,
		Status:         domain.ReconStatusRunning,
		StartedAt:      startTime,
		TotalChecks:    7,
		TotalAnomalies: 0,
		Summary:        json.RawMessage("{}"),
	}

	s.logger.Info("starting reconciliation audit run", "run_id", runID)

	var allFindings []domain.ReconciliationFinding

	err := s.txManager.WithTx(ctx, func(tx repository.DBTX) error {
		// Persist initial RUNNING status
		if err := s.reconRepo.CreateRun(ctx, tx, run); err != nil {
			return fmt.Errorf("failed creating reconciliation run: %w", err)
		}

		// Check 1: Unbalanced ledger transactions
		f1, err := s.reconRepo.CheckUnbalancedTransactions(ctx, tx)
		if err != nil {
			return fmt.Errorf("check 1 (unbalanced transactions) failed: %w", err)
		}
		allFindings = append(allFindings, f1...)

		// Check 2: Captured payments missing ledger capture posting
		f2, err := s.reconRepo.CheckMissingCapturePostings(ctx, tx)
		if err != nil {
			return fmt.Errorf("check 2 (missing capture postings) failed: %w", err)
		}
		allFindings = append(allFindings, f2...)

		// Check 3: Duplicate capture postings
		f3, err := s.reconRepo.CheckDuplicateCapturePostings(ctx, tx)
		if err != nil {
			return fmt.Errorf("check 3 (duplicate capture postings) failed: %w", err)
		}
		allFindings = append(allFindings, f3...)

		// Check 4: Missing refund ledger postings
		f4, err := s.reconRepo.CheckMissingRefundPostings(ctx, tx)
		if err != nil {
			return fmt.Errorf("check 4 (missing refund postings) failed: %w", err)
		}
		allFindings = append(allFindings, f4...)

		// Check 5: Refund amounts exceeding captured amounts
		f5, err := s.reconRepo.CheckRefundExceedsCapture(ctx, tx)
		if err != nil {
			return fmt.Errorf("check 5 (refund exceeds capture) failed: %w", err)
		}
		allFindings = append(allFindings, f5...)

		// Check 6: Payment state vs amounts mismatch
		f6, err := s.reconRepo.CheckPaymentStatusMismatches(ctx, tx)
		if err != nil {
			return fmt.Errorf("check 6 (payment status mismatches) failed: %w", err)
		}
		allFindings = append(allFindings, f6...)

		// Check 7: Orphaned ledger transactions
		f7, err := s.reconRepo.CheckOrphanedLedgerTransactions(ctx, tx)
		if err != nil {
			return fmt.Errorf("check 7 (orphaned ledger transactions) failed: %w", err)
		}
		allFindings = append(allFindings, f7...)

		// Associate findings with this run
		for i := range allFindings {
			allFindings[i].RunID = runID
		}

		// Calculate summary breakdown
		breakdown := make(map[string]int)
		for _, f := range allFindings {
			breakdown[f.AnomalyType]++
		}
		summaryBytes, _ := json.Marshal(map[string]any{
			"anomaly_breakdown": breakdown,
			"passed_checks":     run.TotalChecks - len(breakdown),
		})

		completedAt := time.Now()
		run.Status = domain.ReconStatusCompleted
		run.CompletedAt = &completedAt
		run.TotalAnomalies = len(allFindings)
		run.Summary = summaryBytes
		run.Findings = allFindings

		// Persist findings
		if len(allFindings) > 0 {
			if err := s.reconRepo.CreateFindings(ctx, tx, allFindings); err != nil {
				return fmt.Errorf("failed creating findings: %w", err)
			}
		}

		// Update run to COMPLETED
		if err := s.reconRepo.UpdateRun(ctx, tx, run); err != nil {
			return fmt.Errorf("failed updating run to completed: %w", err)
		}

		return nil
	})

	if err != nil {
		s.logger.Error("reconciliation audit run failed", "run_id", runID, "error", err)
		return nil, err
	}

	s.logger.Info(
		"reconciliation audit run finished",
		"run_id", runID,
		"total_anomalies", run.TotalAnomalies,
		"duration", run.CompletedAt.Sub(startTime).String(),
	)

	return run, nil
}

func (s *ReconciliationService) GetRun(ctx context.Context, id uuid.UUID) (*domain.ReconciliationRun, error) {
	var run *domain.ReconciliationRun
	err := s.txManager.WithTx(ctx, func(tx repository.DBTX) error {
		var err error
		run, err = s.reconRepo.GetRunByID(ctx, tx, id)
		return err
	})
	if err != nil {
		return nil, err
	}
	return run, nil
}
