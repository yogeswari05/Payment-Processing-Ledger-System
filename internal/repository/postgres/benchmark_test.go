package postgres_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/payflow/payflow/internal/domain"
	"github.com/payflow/payflow/internal/repository"
	"github.com/payflow/payflow/internal/repository/postgres"
	"github.com/payflow/payflow/internal/service"
	"github.com/payflow/payflow/internal/testutil"
)

func BenchmarkPaymentCreation(b *testing.B) {
	pool := testutil.SetupTestDB(b)
	ctx := context.Background()

	txManager := postgres.NewTxManager(pool)
	merchantRepo := postgres.NewMerchantRepo()
	paymentRepo := postgres.NewPaymentRepo()
	ledgerRepo := postgres.NewLedgerRepo()
	idempotencyRepo := postgres.NewIdempotencyRepo()
	outboxRepo := postgres.NewOutboxRepo()

	merchantSvc := service.NewMerchantService(txManager, merchantRepo)
	ledgerSvc := service.NewLedgerService(ledgerRepo)
	idempotencySvc := service.NewIdempotencyService(idempotencyRepo, 24)
	paymentSvc := service.NewPaymentService(
		txManager, paymentRepo, merchantRepo, ledgerSvc,
		outboxRepo, idempotencySvc, 290, 30,
	)

	merchant, _, err := merchantSvc.CreateMerchant(ctx, "Bench Merchant")
	if err != nil {
		b.Fatalf("failed creating merchant: %v", err)
	}

	customer, err := merchantSvc.CreateCustomer(ctx, merchant.ID, "bench_cust@test.com", "Bench Customer")
	if err != nil {
		b.Fatalf("failed creating customer: %v", err)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		idemKey := fmt.Sprintf("bench-p-create-%d", i)
		reqHash := domain.ComputeRequestHash("POST", "/v1/payments", []byte(idemKey))

		_, _, err := paymentSvc.CreatePayment(ctx, service.CreatePaymentParams{
			MerchantID:     merchant.ID,
			CustomerID:     customer.ID,
			Amount:         5000,
			Currency:       "USD",
			Description:    "Benchmark payment",
			IdempotencyKey: idemKey,
			RequestHash:    reqHash,
		})
		if err != nil {
			b.Fatalf("payment creation failed: %v", err)
		}
	}
}

func BenchmarkPaymentLookup(b *testing.B) {
	pool := testutil.SetupTestDB(b)
	ctx := context.Background()

	txManager := postgres.NewTxManager(pool)
	merchantRepo := postgres.NewMerchantRepo()
	paymentRepo := postgres.NewPaymentRepo()
	ledgerRepo := postgres.NewLedgerRepo()
	idempotencyRepo := postgres.NewIdempotencyRepo()
	outboxRepo := postgres.NewOutboxRepo()

	merchantSvc := service.NewMerchantService(txManager, merchantRepo)
	ledgerSvc := service.NewLedgerService(ledgerRepo)
	idempotencySvc := service.NewIdempotencyService(idempotencyRepo, 24)
	paymentSvc := service.NewPaymentService(
		txManager, paymentRepo, merchantRepo, ledgerSvc,
		outboxRepo, idempotencySvc, 290, 30,
	)

	merchant, _, _ := merchantSvc.CreateMerchant(ctx, "Lookup Merchant")
	customer, _ := merchantSvc.CreateCustomer(ctx, merchant.ID, "lookup@test.com", "Lookup Customer")

	p, _, err := paymentSvc.CreatePayment(ctx, service.CreatePaymentParams{
		MerchantID:     merchant.ID,
		CustomerID:     customer.ID,
		Amount:         10000,
		Currency:       "USD",
		IdempotencyKey: "bench-lookup-target",
		RequestHash:    "bench-hash",
	})
	if err != nil {
		b.Fatalf("setup failed: %v", err)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := paymentSvc.GetPayment(ctx, merchant.ID, p.ID)
		if err != nil {
			b.Fatalf("lookup failed: %v", err)
		}
	}
}

func BenchmarkIdempotentReplay(b *testing.B) {
	pool := testutil.SetupTestDB(b)
	ctx := context.Background()

	txManager := postgres.NewTxManager(pool)
	merchantRepo := postgres.NewMerchantRepo()
	paymentRepo := postgres.NewPaymentRepo()
	ledgerRepo := postgres.NewLedgerRepo()
	idempotencyRepo := postgres.NewIdempotencyRepo()
	outboxRepo := postgres.NewOutboxRepo()

	merchantSvc := service.NewMerchantService(txManager, merchantRepo)
	ledgerSvc := service.NewLedgerService(ledgerRepo)
	idempotencySvc := service.NewIdempotencyService(idempotencyRepo, 24)
	paymentSvc := service.NewPaymentService(
		txManager, paymentRepo, merchantRepo, ledgerSvc,
		outboxRepo, idempotencySvc, 290, 30,
	)

	merchant, _, _ := merchantSvc.CreateMerchant(ctx, "Replay Merchant")
	customer, _ := merchantSvc.CreateCustomer(ctx, merchant.ID, "replay@test.com", "Replay Cust")

	idemKey := "replay-target-key"
	reqHash := domain.ComputeRequestHash("POST", "/v1/payments", []byte("replay-body"))

	// Create once
	_, _, err := paymentSvc.CreatePayment(ctx, service.CreatePaymentParams{
		MerchantID:     merchant.ID,
		CustomerID:     customer.ID,
		Amount:         2500,
		Currency:       "USD",
		IdempotencyKey: idemKey,
		RequestHash:    reqHash,
	})
	if err != nil {
		b.Fatalf("failed initial payment: %v", err)
	}

	b.ResetTimer()
	b.ReportAllocs()

	// Replay repeatedly
	for i := 0; i < b.N; i++ {
		_, isReplay, err := paymentSvc.CreatePayment(ctx, service.CreatePaymentParams{
			MerchantID:     merchant.ID,
			CustomerID:     customer.ID,
			Amount:         2500,
			Currency:       "USD",
			IdempotencyKey: idemKey,
			RequestHash:    reqHash,
		})
		if err != nil || !isReplay {
			b.Fatalf("expected successful idempotent replay, got isReplay=%v, err=%v", isReplay, err)
		}
	}
}

func BenchmarkLedgerPosting(b *testing.B) {
	pool := testutil.SetupTestDB(b)
	ctx := context.Background()

	txManager := postgres.NewTxManager(pool)
	merchantRepo := postgres.NewMerchantRepo()
	paymentRepo := postgres.NewPaymentRepo()
	ledgerRepo := postgres.NewLedgerRepo()
	idempotencyRepo := postgres.NewIdempotencyRepo()
	outboxRepo := postgres.NewOutboxRepo()

	merchantSvc := service.NewMerchantService(txManager, merchantRepo)
	ledgerSvc := service.NewLedgerService(ledgerRepo)
	idempotencySvc := service.NewIdempotencyService(idempotencyRepo, 24)
	paymentSvc := service.NewPaymentService(
		txManager, paymentRepo, merchantRepo, ledgerSvc,
		outboxRepo, idempotencySvc, 290, 30,
	)

	merchant, _, _ := merchantSvc.CreateMerchant(ctx, "Ledger Bench Merchant")
	customer, _ := merchantSvc.CreateCustomer(ctx, merchant.ID, "ledger@test.com", "Ledger Cust")

	// Pre-create authorized payments for capture
	payments := make([]*domain.Payment, b.N)
	for i := 0; i < b.N; i++ {
		p, _, err := paymentSvc.CreatePayment(ctx, service.CreatePaymentParams{
			MerchantID:     merchant.ID,
			CustomerID:     customer.ID,
			Amount:         10000,
			Currency:       "USD",
			IdempotencyKey: fmt.Sprintf("p-auth-bench-%d", i),
			RequestHash:    "h",
		})
		if err != nil {
			b.Fatalf("create failed: %v", err)
		}
		pAuth, _, err := paymentSvc.ConfirmPayment(ctx, merchant.ID, p.ID, fmt.Sprintf("auth-key-%d", i), "h")
		if err != nil {
			b.Fatalf("auth failed: %v", err)
		}
		payments[i] = pAuth
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		idemKey := fmt.Sprintf("cap-bench-%d", i)
		reqHash := domain.ComputeRequestHash("POST", "/v1/payments/capture", []byte(idemKey))
		_, _, err := paymentSvc.CapturePayment(ctx, merchant.ID, payments[i].ID, nil, idemKey, reqHash)
		if err != nil {
			b.Fatalf("capture/ledger posting failed: %v", err)
		}
	}
}

func BenchmarkOutboxProcessing(b *testing.B) {
	pool := testutil.SetupTestDB(b)
	ctx := context.Background()

	txManager := postgres.NewTxManager(pool)
	outboxRepo := postgres.NewOutboxRepo()
	logger := testutil.NewTestLogger()

	worker := service.NewOutboxWorker(
		pool, txManager, outboxRepo, "bench-worker",
		10*time.Millisecond, 50, logger,
	)

	// Pre-insert b.N outbox events
	now := time.Now()
	for i := 0; i < b.N; i++ {
		evt := &domain.OutboxEvent{
			ID:            uuid.New(),
			EventType:     "benchmark.event",
			AggregateType: "benchmark",
			AggregateID:   uuid.New(),
			Payload:       json.RawMessage(`{"benchmark": true}`),
			Status:        domain.OutboxStatusPending,
			Attempts:      0,
			MaxAttempts:   5,
			NextRetryAt:   now,
			CreatedAt:     now,
		}
		_ = txManager.WithTx(ctx, func(tx repository.DBTX) error {
			return outboxRepo.Create(ctx, tx, evt)
		})
	}

	b.ResetTimer()
	b.ReportAllocs()

	totalProcessed := 0
	for totalProcessed < b.N {
		count, err := worker.ProcessOnce(ctx)
		if err != nil {
			b.Fatalf("worker batch process failed: %v", err)
		}
		totalProcessed += count
		if count == 0 {
			break
		}
	}
}

func BenchmarkConcurrentCaptureAndRefund(b *testing.B) {
	pool := testutil.SetupTestDB(b)
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
		outboxRepo, idempotencySvc, 290, 30,
	)
	refundSvc := service.NewRefundService(
		txManager, paymentRepo, refundRepo, ledgerSvc,
		outboxRepo, idempotencySvc,
	)

	merchant, _, _ := merchantSvc.CreateMerchant(ctx, "Concurrent Bench Merchant")
	customer, _ := merchantSvc.CreateCustomer(ctx, merchant.ID, "bench_conc@test.com", "Conc Cust")

	b.ResetTimer()
	b.ReportAllocs()

	var completedOps atomic.Int64

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			idx := completedOps.Add(1)
			idemCreate := fmt.Sprintf("conc-create-%d", idx)
			p, _, err := paymentSvc.CreatePayment(ctx, service.CreatePaymentParams{
				MerchantID:     merchant.ID,
				CustomerID:     customer.ID,
				Amount:         10000,
				Currency:       "USD",
				IdempotencyKey: idemCreate,
				RequestHash:    "h",
			})
			if err != nil {
				continue
			}

			_, _, _ = paymentSvc.ConfirmPayment(ctx, merchant.ID, p.ID, fmt.Sprintf("conc-auth-%d", idx), "h")
			_, _, _ = paymentSvc.CapturePayment(ctx, merchant.ID, p.ID, nil, fmt.Sprintf("conc-cap-%d", idx), "h")
			_, _, _ = refundSvc.CreateRefund(ctx, service.CreateRefundParams{
				MerchantID:     merchant.ID,
				PaymentID:      p.ID,
				Amount:         5000,
				IdempotencyKey: fmt.Sprintf("conc-ref-%d", idx),
				RequestHash:    "h",
			})
		}
	})
}
