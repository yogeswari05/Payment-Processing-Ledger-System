package api

import (
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/payflow/payflow/internal/metrics"
	"github.com/payflow/payflow/internal/service"
)

type Handlers struct {
	Merchant       *MerchantHandler
	Payment        *PaymentHandler
	Refund         *RefundHandler
	Ledger         *LedgerHandler
	Reconciliation *ReconciliationHandler
	Health         *HealthHandler
}

// NewRouter constructs and configures the HTTP router using Go standard library net/http ServeMux.
func NewRouter(
	handlers Handlers,
	merchantSvc *service.MerchantService,
	pool *pgxpool.Pool,
	metrics *metrics.Metrics,
	logger *slog.Logger,
) http.Handler {
	mux := http.NewServeMux()

	// 1. Operational & Observability endpoints (Unauthenticated)
	mux.HandleFunc("GET /healthz", handlers.Health.Healthz)
	mux.HandleFunc("GET /readyz", handlers.Health.Readyz)
	mux.Handle("GET /metrics", promhttp.Handler())

	// 2. Merchant creation / onboarding (Public)
	mux.HandleFunc("POST /v1/merchants", handlers.Merchant.CreateMerchant)

	// 3. Reconciliation endpoints (Internal ops)
	mux.HandleFunc("POST /internal/reconciliation/runs", handlers.Reconciliation.TriggerRun)
	mux.HandleFunc("GET /internal/reconciliation/runs/{run_id}", handlers.Reconciliation.GetRun)

	// 4. Authenticated API Endpoints (wrapped with AuthMiddleware)
	authWrapper := AuthMiddleware(merchantSvc)

	// Customers
	mux.Handle("POST /v1/customers", authWrapper(http.HandlerFunc(handlers.Merchant.CreateCustomer)))
	mux.Handle("GET /v1/customers/{customer_id}", authWrapper(http.HandlerFunc(handlers.Merchant.GetCustomer)))

	// Payments
	mux.Handle("POST /v1/payments", authWrapper(http.HandlerFunc(handlers.Payment.CreatePayment)))
	mux.Handle("GET /v1/payments/{payment_id}", authWrapper(http.HandlerFunc(handlers.Payment.GetPayment)))
	mux.Handle("GET /v1/payments", authWrapper(http.HandlerFunc(handlers.Payment.ListPayments)))
	mux.Handle("POST /v1/payments/{payment_id}/confirm", authWrapper(http.HandlerFunc(handlers.Payment.ConfirmPayment)))
	mux.Handle("POST /v1/payments/{payment_id}/capture", authWrapper(http.HandlerFunc(handlers.Payment.CapturePayment)))
	mux.Handle("POST /v1/payments/{payment_id}/cancel", authWrapper(http.HandlerFunc(handlers.Payment.CancelPayment)))

	// Refunds
	mux.Handle("POST /v1/payments/{payment_id}/refunds", authWrapper(http.HandlerFunc(handlers.Refund.CreateRefund)))
	mux.Handle("GET /v1/refunds/{refund_id}", authWrapper(http.HandlerFunc(handlers.Refund.GetRefund)))

	// Ledger
	mux.Handle("GET /v1/ledger/accounts", authWrapper(http.HandlerFunc(handlers.Ledger.ListAccounts)))
	mux.Handle("GET /v1/ledger/transactions/{transaction_id}", authWrapper(http.HandlerFunc(handlers.Ledger.GetTransaction)))
	mux.Handle("GET /v1/ledger/entries", authWrapper(http.HandlerFunc(handlers.Ledger.ListEntries)))

	// Apply global middlewares in order: Recovery -> RequestLogger -> Metrics -> MaxBytes -> Router
	var handler http.Handler = mux
	handler = MaxBytesMiddleware(1024 * 1024)(handler) // 1MB body limit
	handler = MetricsMiddleware(metrics)(handler)
	handler = RequestLogger(logger)(handler)
	handler = RecoveryMiddleware(logger)(handler)

	return handler
}
