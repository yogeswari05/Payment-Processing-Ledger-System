package postgres_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/payflow/payflow/internal/domain"
	"github.com/payflow/payflow/internal/repository"
	"github.com/payflow/payflow/internal/repository/postgres"
	"github.com/payflow/payflow/internal/service"
	"github.com/payflow/payflow/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPostgresIntegration_LifecycleAndInvariants(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	txManager := postgres.NewTxManager(pool)
	merchantRepo := postgres.NewMerchantRepo()
	paymentRepo := postgres.NewPaymentRepo()
	refundRepo := postgres.NewRefundRepo()
	ledgerRepo := postgres.NewLedgerRepo()
	idempotencyRepo := postgres.NewIdempotencyRepo()
	outboxRepo := postgres.NewOutboxRepo()
	reconRepo := postgres.NewReconciliationRepo()

	merchantSvc := service.NewMerchantService(txManager, merchantRepo)
	ledgerSvc := service.NewLedgerService(ledgerRepo)
	idempotencySvc := service.NewIdempotencyService(idempotencyRepo, 24)
	paymentSvc := service.NewPaymentService(
		txManager, paymentRepo, merchantRepo, ledgerSvc,
		outboxRepo, idempotencySvc, 290, 30, // 2.9% + 30c
	)
	refundSvc := service.NewRefundService(
		txManager, paymentRepo, refundRepo, ledgerSvc,
		outboxRepo, idempotencySvc,
	)

	// 1. Create Merchant & Customer
	merchant, rawKey, err := merchantSvc.CreateMerchant(ctx, "Stripe Test Merchant")
	require.NoError(t, err)
	require.NotEmpty(t, rawKey)

	authedMerchant, err := merchantSvc.Authenticate(ctx, rawKey)
	require.NoError(t, err)
	assert.Equal(t, merchant.ID, authedMerchant.ID)

	customer, err := merchantSvc.CreateCustomer(ctx, merchant.ID, "alice@example.com", "Alice Smith")
	require.NoError(t, err)
	assert.Equal(t, merchant.ID, customer.MerchantID)

	// 2. Create Payment ($100.00 = 10000 cents)
	createIdemKey := "idem-create-1"
	reqHash1 := domain.ComputeRequestHash("POST", "/v1/payments", []byte("req1"))
	payment, isReplay, err := paymentSvc.CreatePayment(ctx, service.CreatePaymentParams{
		MerchantID:     merchant.ID,
		CustomerID:     customer.ID,
		Amount:         10000,
		Currency:       "USD",
		Description:    "Integration test payment",
		IdempotencyKey: createIdemKey,
		RequestHash:    reqHash1,
	})
	require.NoError(t, err)
	assert.False(t, isReplay)
	assert.Equal(t, domain.PaymentStatusCreated, payment.Status)
	assert.Equal(t, int64(10000), payment.Amount)

	// 2b. Test Idempotency Replay on Payment Creation
	replayedPayment, isReplay2, err := paymentSvc.CreatePayment(ctx, service.CreatePaymentParams{
		MerchantID:     merchant.ID,
		CustomerID:     customer.ID,
		Amount:         10000,
		Currency:       "USD",
		IdempotencyKey: createIdemKey,
		RequestHash:    reqHash1,
	})
	require.NoError(t, err)
	assert.True(t, isReplay2)
	assert.Equal(t, payment.ID, replayedPayment.ID)

	// 2c. Test Idempotency Key Mismatch with different request payload
	diffHash := domain.ComputeRequestHash("POST", "/v1/payments", []byte("diff-req"))
	_, _, err = paymentSvc.CreatePayment(ctx, service.CreatePaymentParams{
		MerchantID:     merchant.ID,
		CustomerID:     customer.ID,
		Amount:         20000,
		Currency:       "USD",
		IdempotencyKey: createIdemKey,
		RequestHash:    diffHash,
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, domain.ErrIdempotencyMismatch)

	// 3. Confirm / Authorize Payment
	authIdemKey := "idem-auth-1"
	authHash := domain.ComputeRequestHash("POST", "/v1/payments/confirm", []byte{})
	authorizedPayment, _, err := paymentSvc.ConfirmPayment(ctx, merchant.ID, payment.ID, authIdemKey, authHash)
	require.NoError(t, err)
	assert.Equal(t, domain.PaymentStatusAuthorized, authorizedPayment.Status)

	// 4. Capture Payment ($100.00)
	capIdemKey := "idem-cap-1"
	capHash := domain.ComputeRequestHash("POST", "/v1/payments/capture", []byte{})
	capturedPayment, _, err := paymentSvc.CapturePayment(ctx, merchant.ID, payment.ID, nil, capIdemKey, capHash)
	require.NoError(t, err)
	assert.Equal(t, domain.PaymentStatusCaptured, capturedPayment.Status)
	assert.Equal(t, int64(10000), capturedPayment.CapturedAmount)

	// 5. Verify Double-Entry Ledger Posting for Capture
	// Fee = (10000 * 290 / 10000) + 30 = 290 + 30 = 320 cents ($3.20)
	// Merchant net = 10000 - 320 = 9680 cents ($96.80)
	// Debits = 10000, Credits = 9680 + 320 = 10000. Balanced!
	ledgerTx, err := ledgerRepo.GetTransactionByBusinessRef(ctx, pool, domain.RefTypePaymentCapture, payment.ID)
	require.NoError(t, err)
	assert.Equal(t, "POSTED", ledgerTx.Status)
	assert.Equal(t, "USD", ledgerTx.Currency)

	// Fetch entries
	entries, _, err := ledgerRepo.ListEntries(ctx, pool, repository.ListEntriesFilter{
		TransactionID: &ledgerTx.ID,
	})
	require.NoError(t, err)
	assert.Len(t, entries, 3)

	var totalDebit, totalCredit int64
	for _, e := range entries {
		if e.Direction == domain.DirectionDebit {
			totalDebit += e.Amount
		} else {
			totalCredit += e.Amount
		}
	}
	assert.Equal(t, int64(10000), totalDebit)
	assert.Equal(t, int64(10000), totalCredit)
	assert.Equal(t, totalDebit, totalCredit, "Double-entry equation MUST balance!")

	// 6. Partial Refund 1: $40.00 (4000 cents)
	refIdemKey1 := "idem-ref-1"
	refHash1 := domain.ComputeRequestHash("POST", "/v1/payments/refunds", []byte("ref1"))
	refund1, isRefReplay1, err := refundSvc.CreateRefund(ctx, service.CreateRefundParams{
		MerchantID:     merchant.ID,
		PaymentID:      payment.ID,
		Amount:         4000,
		Reason:         "Customer requested partial refund",
		IdempotencyKey: refIdemKey1,
		RequestHash:    refHash1,
	})
	require.NoError(t, err)
	assert.False(t, isRefReplay1)
	assert.Equal(t, domain.RefundStatusSucceeded, refund1.Status)

	// Verify Payment is now partially_refunded with refunded_amount = 4000
	updatedPayment1, err := paymentSvc.GetPayment(ctx, merchant.ID, payment.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.PaymentStatusPartiallyRefunded, updatedPayment1.Status)
	assert.Equal(t, int64(4000), updatedPayment1.RefundedAmount)

	// 7. Partial Refund 2: $60.00 (6000 cents) - Fully completes the refund
	refIdemKey2 := "idem-ref-2"
	refHash2 := domain.ComputeRequestHash("POST", "/v1/payments/refunds", []byte("ref2"))
	refund2, _, err := refundSvc.CreateRefund(ctx, service.CreateRefundParams{
		MerchantID:     merchant.ID,
		PaymentID:      payment.ID,
		Amount:         6000,
		Reason:         "Second partial refund completing total",
		IdempotencyKey: refIdemKey2,
		RequestHash:    refHash2,
	})
	require.NoError(t, err)
	assert.Equal(t, domain.RefundStatusSucceeded, refund2.Status)

	// Verify Payment is now refunded with refunded_amount = 10000
	updatedPayment2, err := paymentSvc.GetPayment(ctx, merchant.ID, payment.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.PaymentStatusRefunded, updatedPayment2.Status)
	assert.Equal(t, int64(10000), updatedPayment2.RefundedAmount)

	// 8. Attempt 3rd refund (should fail: exceeds refundable amount)
	refIdemKey3 := "idem-ref-3"
	refHash3 := domain.ComputeRequestHash("POST", "/v1/payments/refunds", []byte("ref3"))
	_, _, err = refundSvc.CreateRefund(ctx, service.CreateRefundParams{
		MerchantID:     merchant.ID,
		PaymentID:      payment.ID,
		Amount:         100, // Even 1 cent should be rejected!
		Reason:         "Over refund attempt",
		IdempotencyKey: refIdemKey3,
		RequestHash:    refHash3,
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, domain.ErrPaymentNotCaptured) // Payment in refunded status cannot be refunded further

	// 9. Run Reconciliation Service and verify 0 anomalies detected!
	reconSvc := service.NewReconciliationService(txManager, reconRepo, testutil.NewTestLogger())
	reconRun, err := reconSvc.RunAudit(ctx)
	require.NoError(t, err)
	assert.Equal(t, domain.ReconStatusCompleted, reconRun.Status)
	assert.Equal(t, 0, reconRun.TotalAnomalies, "Healthy system must have 0 anomalies!")
	assert.Empty(t, reconRun.Findings)
}

func TestPostgresIntegration_ConcurrentCaptureAndRefund(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	txManager := postgres.NewTxManager(pool)
	merchantRepo := postgres.NewMerchantRepo()
	paymentRepo := postgres.NewPaymentRepo()
	refundRepo := postgres.NewRefundRepo()
	ledgerRepo := postgres.NewLedgerRepo()
	idempotencyRepo := postgres.NewIdempotencyRepo()
	outboxRepo := postgres.NewOutboxRepo()

	merchantSvc := service.NewMerchantService(txManager, merchantRepo)
	ledgerSvc := service.NewLedgerService(ledgerRepo)
	idempotencySvc := service.NewIdempotencyService(idempotencyRepo, 24)
	paymentSvc := service.NewPaymentService(
		txManager, paymentRepo, merchantRepo, ledgerSvc,
		outboxRepo, idempotencySvc, 0, 0,
	)
	refundSvc := service.NewRefundService(
		txManager, paymentRepo, refundRepo, ledgerSvc,
		outboxRepo, idempotencySvc,
	)

	merchant, _, err := merchantSvc.CreateMerchant(ctx, "Concurrency Merchant")
	require.NoError(t, err)
	customer, err := merchantSvc.CreateCustomer(ctx, merchant.ID, "bob@example.com", "Bob")
	require.NoError(t, err)

	// Create and Authorize a payment of $100 (10000 cents)
	p, _, err := paymentSvc.CreatePayment(ctx, service.CreatePaymentParams{
		MerchantID:     merchant.ID,
		CustomerID:     customer.ID,
		Amount:         10000,
		Currency:       "USD",
		IdempotencyKey: "p-conc-1",
		RequestHash:    "h1",
	})
	require.NoError(t, err)

	_, _, err = paymentSvc.ConfirmPayment(ctx, merchant.ID, p.ID, "p-auth-1", "h2")
	require.NoError(t, err)

	// Concurrently attempt to capture the payment 5 times with DIFFERENT idempotency keys
	var wg sync.WaitGroup
	captureSuccessCount := 0
	captureFailCount := 0
	var mu sync.Mutex

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			idemKey := uuid.New().String()
			reqHash := domain.ComputeRequestHash("POST", "/v1/payments/capture", []byte(idemKey))
			_, _, capErr := paymentSvc.CapturePayment(ctx, merchant.ID, p.ID, nil, idemKey, reqHash)

			mu.Lock()
			if capErr == nil {
				captureSuccessCount++
			} else {
				captureFailCount++
			}
			mu.Unlock()
		}(i)
	}
	wg.Wait()

	// Exactly ONE capture must succeed; all other concurrent captures must be rejected!
	assert.Equal(t, 1, captureSuccessCount, "Exactly 1 concurrent capture must succeed")
	assert.Equal(t, 4, captureFailCount, "Other 4 captures must be rejected due to state machine locks")

	// Now payment is captured for $100.
	// Concurrently attempt two refunds of $70 each.
	// Since 70 + 70 = 140 > 100, exactly ONE refund of $70 must succeed and the second must be rejected!
	refundSuccessCount := 0
	refundFailCount := 0

	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			idemKey := uuid.New().String()
			reqHash := domain.ComputeRequestHash("POST", "/v1/payments/refunds", []byte(idemKey))
			_, _, refErr := refundSvc.CreateRefund(ctx, service.CreateRefundParams{
				MerchantID:     merchant.ID,
				PaymentID:      p.ID,
				Amount:         7000, // $70.00
				Reason:         "Concurrent race test",
				IdempotencyKey: idemKey,
				RequestHash:    reqHash,
			})

			mu.Lock()
			if refErr == nil {
				refundSuccessCount++
			} else {
				refundFailCount++
			}
			mu.Unlock()
		}(i)
	}
	wg.Wait()

	assert.Equal(t, 1, refundSuccessCount, "Exactly 1 refund of $70 must succeed")
	assert.Equal(t, 1, refundFailCount, "Second refund of $70 must fail because available is only $30")

	// Final verification of payment state
	finalPayment, err := paymentSvc.GetPayment(ctx, merchant.ID, p.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(7000), finalPayment.RefundedAmount)
	assert.Equal(t, domain.PaymentStatusPartiallyRefunded, finalPayment.Status)
}

func TestPostgresIntegration_ReconciliationDetectsInjectedAnomalies(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	txManager := postgres.NewTxManager(pool)
	reconRepo := postgres.NewReconciliationRepo()
	reconSvc := service.NewReconciliationService(txManager, reconRepo, testutil.NewTestLogger())

	// Deliberately inject 3 distinct anomalies into the database:
	// Anomaly 1: Unbalanced ledger transaction (Debit 1000, Credit 800)
	txID := uuid.New()
	acc1 := uuid.New()
	acc2 := uuid.New()

	_, err := pool.Exec(ctx, `
		INSERT INTO ledger_accounts (id, account_type, name, currency, created_at)
		VALUES
			($1, 'asset', 'unbalanced_acc1', 'USD', NOW()),
			($2, 'liability', 'unbalanced_acc2', 'USD', NOW())
	`, acc1, acc2)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO ledger_transactions (id, business_reference_type, business_reference_id, transaction_type, currency, status, created_at)
		VALUES ($1, 'ADJUSTMENT', $1, 'TEST_ANOMALY', 'USD', 'POSTED', NOW())
	`, txID)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO ledger_entries (id, ledger_transaction_id, ledger_account_id, direction, amount, currency, created_at)
		VALUES
			(gen_random_uuid(), $1, $2, 'DEBIT', 1000, 'USD', NOW()),
			(gen_random_uuid(), $1, $3, 'CREDIT', 800, 'USD', NOW())
	`, txID, acc1, acc2)
	require.NoError(t, err)

	// Anomaly 2: Captured payment missing ledger capture posting
	merchantID := uuid.New()
	customerID := uuid.New()
	paymentID := uuid.New()

	_, err = pool.Exec(ctx, `
		INSERT INTO merchants (id, name, status, api_key_hash, created_at, updated_at)
		VALUES ($1, 'Anomaly Merchant', 'active', 'hash_test', NOW(), NOW())
	`, merchantID)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO customers (id, merchant_id, email, name, created_at, updated_at)
		VALUES ($1, $2, 'anomaly@test.com', 'Anomaly Customer', NOW(), NOW())
	`, customerID, merchantID)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO payments (id, merchant_id, customer_id, amount, currency, status, captured_amount, refunded_amount, created_at, updated_at)
		VALUES ($1, $2, $3, 5000, 'USD', 'captured', 5000, 0, NOW(), NOW())
	`, paymentID, merchantID, customerID)
	require.NoError(t, err)

	// Anomaly 3: Payment status mismatch (status 'captured' but captured_amount == 0)
	mismatchPaymentID := uuid.New()
	_, err = pool.Exec(ctx, `
		INSERT INTO payments (id, merchant_id, customer_id, amount, currency, status, captured_amount, refunded_amount, created_at, updated_at)
		VALUES ($1, $2, $3, 3000, 'USD', 'captured', 0, 0, NOW(), NOW())
	`, mismatchPaymentID, merchantID, customerID)
	require.NoError(t, err)

	// Execute Reconciliation
	run, err := reconSvc.RunAudit(ctx)
	require.NoError(t, err)
	assert.Equal(t, domain.ReconStatusCompleted, run.Status)
	assert.GreaterOrEqual(t, run.TotalAnomalies, 3, "Reconciliation must detect all injected anomalies!")

	foundUnbalanced := false
	foundMissingCapture := false
	foundStatusMismatch := false

	for _, f := range run.Findings {
		switch f.AnomalyType {
		case domain.AnomalyUnbalancedLedgerTransaction:
			if f.EntityID == txID {
				foundUnbalanced = true
			}
		case domain.AnomalyMissingCapturePosting:
			if f.EntityID == paymentID {
				foundMissingCapture = true
			}
		case domain.AnomalyPaymentStatusMismatch:
			if f.EntityID == mismatchPaymentID {
				foundStatusMismatch = true
			}
		}
	}

	assert.True(t, foundUnbalanced, "Reconciliation must detect unbalanced transaction %s", txID)
	assert.True(t, foundMissingCapture, "Reconciliation must detect missing capture posting for %s", paymentID)
	assert.True(t, foundStatusMismatch, "Reconciliation must detect payment status mismatch for %s", mismatchPaymentID)
}

func TestPostgresIntegration_OutboxWorkerRecoveryAndDeduplication(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	txManager := postgres.NewTxManager(pool)
	outboxRepo := postgres.NewOutboxRepo()
	logger := testutil.NewTestLogger()

	worker := service.NewOutboxWorker(
		pool, txManager, outboxRepo, "test-worker-1",
		100*time.Millisecond, 10, logger,
	)

	// 1. Insert 3 pending outbox events
	now := time.Now()
	evt1 := &domain.OutboxEvent{
		ID:            uuid.New(),
		EventType:     "test.event.1",
		AggregateType: "test",
		AggregateID:   uuid.New(),
		Payload:       json.RawMessage(`{"num": 1}`),
		Status:        domain.OutboxStatusPending,
		Attempts:      0,
		MaxAttempts:   5,
		NextRetryAt:   now,
		CreatedAt:     now,
	}
	evt2 := &domain.OutboxEvent{
		ID:            uuid.New(),
		EventType:     "test.event.2",
		AggregateType: "test",
		AggregateID:   uuid.New(),
		Payload:       json.RawMessage(`{"num": 2}`),
		Status:        domain.OutboxStatusPending,
		Attempts:      0,
		MaxAttempts:   5,
		NextRetryAt:   now,
		CreatedAt:     now,
	}

	err := txManager.WithTx(ctx, func(tx repository.DBTX) error {
		if err := outboxRepo.Create(ctx, tx, evt1); err != nil {
			return err
		}
		return outboxRepo.Create(ctx, tx, evt2)
	})
	require.NoError(t, err)

	// 2. Process batch synchronously using ProcessOnce
	processedCount, err := worker.ProcessOnce(ctx)
	require.NoError(t, err)
	assert.Equal(t, 2, processedCount)

	// 3. Verify events are now marked PUBLISHED
	var status1, status2 string
	err = pool.QueryRow(ctx, "SELECT status FROM outbox_events WHERE id = $1", evt1.ID).Scan(&status1)
	require.NoError(t, err)
	assert.Equal(t, "PUBLISHED", status1)

	err = pool.QueryRow(ctx, "SELECT status FROM outbox_events WHERE id = $1", evt2.ID).Scan(&status2)
	require.NoError(t, err)
	assert.Equal(t, "PUBLISHED", status2)

	// 4. Verify exactly-once consumer deduplication in consumed_events
	firstConsume, err := outboxRepo.RecordConsumed(ctx, pool, evt1.ID, "sim_consumer")
	require.NoError(t, err)
	assert.False(t, firstConsume, "Event was already consumed by worker, duplicate delivery must return false!")
}
