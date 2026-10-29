package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/payflow/payflow/internal/domain"
	"github.com/payflow/payflow/internal/repository"
)

type PaymentService struct {
	txManager       repository.TxManager
	paymentRepo     repository.PaymentRepo
	customerRepo    repository.MerchantRepo
	ledgerService   *LedgerService
	outboxRepo      repository.OutboxRepo
	idempotencySvc  *IdempotencyService
	platformFeeBps  int64
	platformFeeFix  int64
}

func NewPaymentService(
	tm repository.TxManager,
	pr repository.PaymentRepo,
	mr repository.MerchantRepo,
	ls *LedgerService,
	or repository.OutboxRepo,
	is *IdempotencyService,
	feeBps int64,
	feeFix int64,
) *PaymentService {
	return &PaymentService{
		txManager:      tm,
		paymentRepo:    pr,
		customerRepo:   mr,
		ledgerService:  ls,
		outboxRepo:     or,
		idempotencySvc: is,
		platformFeeBps: feeBps,
		platformFeeFix: feeFix,
	}
}

type CreatePaymentParams struct {
	MerchantID     uuid.UUID
	CustomerID     uuid.UUID
	Amount         int64
	Currency       string
	Description    string
	Metadata       json.RawMessage
	IdempotencyKey string
	RequestHash    string
}

func (s *PaymentService) CreatePayment(ctx context.Context, params CreatePaymentParams) (*domain.Payment, bool, error) {
	if err := domain.ValidateAmount(params.Amount); err != nil {
		return nil, false, err
	}
	currency, err := domain.ValidateCurrency(params.Currency)
	if err != nil {
		return nil, false, err
	}

	var payment *domain.Payment
	var isReplay bool

	execErr := s.txManager.WithTx(ctx, func(tx repository.DBTX) error {
		res, err := s.idempotencySvc.Execute(
			ctx, tx, params.MerchantID, params.IdempotencyKey, params.RequestHash,
			func() (int, any, *uuid.UUID, error) {
				// Verify customer belongs to merchant
				if _, err := s.customerRepo.GetCustomerByID(ctx, tx, params.MerchantID, params.CustomerID); err != nil {
					return 0, nil, nil, err
				}

				now := time.Now()
				paymentID := uuid.New()
				meta := params.Metadata
				if len(meta) == 0 {
					meta = json.RawMessage("{}")
				}

				p := &domain.Payment{
					ID:             paymentID,
					MerchantID:     params.MerchantID,
					CustomerID:     params.CustomerID,
					Amount:         params.Amount,
					Currency:       currency,
					Status:         domain.PaymentStatusCreated,
					CapturedAmount: 0,
					RefundedAmount: 0,
					Description:    params.Description,
					Metadata:       meta,
					Version:        1,
					CreatedAt:      now,
					UpdatedAt:      now,
				}

				if err := s.paymentRepo.Create(ctx, tx, p); err != nil {
					return 0, nil, nil, fmt.Errorf("failed creating payment: %w", err)
				}

				payload, _ := json.Marshal(p)
				outboxEvt := &domain.OutboxEvent{
					ID:            uuid.New(),
					EventType:     domain.EventPaymentCreated,
					AggregateType: "payment",
					AggregateID:   p.ID,
					Payload:       payload,
					Status:        domain.OutboxStatusPending,
					Attempts:      0,
					MaxAttempts:   5,
					NextRetryAt:   now,
					CreatedAt:     now,
				}
				if err := s.outboxRepo.Create(ctx, tx, outboxEvt); err != nil {
					return 0, nil, nil, fmt.Errorf("failed recording outbox event: %w", err)
				}

				return 201, p, &p.ID, nil
			},
		)
		if err != nil {
			return err
		}

		isReplay = res.IsReplay
		payment = &domain.Payment{}
		if err := json.Unmarshal(res.ResponseBody, payment); err != nil {
			return fmt.Errorf("failed unmarshaling stored payment response: %w", err)
		}
		return nil
	})

	if execErr != nil {
		return nil, false, execErr
	}

	return payment, isReplay, nil
}

func (s *PaymentService) ConfirmPayment(ctx context.Context, merchantID, paymentID uuid.UUID, idemKey, requestHash string) (*domain.Payment, bool, error) {
	var payment *domain.Payment
	var isReplay bool

	execErr := s.txManager.WithTx(ctx, func(tx repository.DBTX) error {
		res, err := s.idempotencySvc.Execute(
			ctx, tx, merchantID, idemKey, requestHash,
			func() (int, any, *uuid.UUID, error) {
				p, err := s.paymentRepo.GetByIDForUpdate(ctx, tx, merchantID, paymentID)
				if err != nil {
					return 0, nil, nil, err
				}

				if !p.CanTransitionTo(domain.PaymentStatusAuthorized) {
					return 0, nil, nil, fmt.Errorf("%w: cannot authorize payment in '%s' status", domain.ErrInvalidStateTransition, p.Status)
				}

				p.Status = domain.PaymentStatusAuthorized
				if err := s.paymentRepo.Update(ctx, tx, p); err != nil {
					return 0, nil, nil, err
				}

				now := time.Now()
				payload, _ := json.Marshal(p)
				outboxEvt := &domain.OutboxEvent{
					ID:            uuid.New(),
					EventType:     domain.EventPaymentAuthorized,
					AggregateType: "payment",
					AggregateID:   p.ID,
					Payload:       payload,
					Status:        domain.OutboxStatusPending,
					Attempts:      0,
					MaxAttempts:   5,
					NextRetryAt:   now,
					CreatedAt:     now,
				}
				if err := s.outboxRepo.Create(ctx, tx, outboxEvt); err != nil {
					return 0, nil, nil, err
				}

				return 200, p, &p.ID, nil
			},
		)
		if err != nil {
			return err
		}

		isReplay = res.IsReplay
		payment = &domain.Payment{}
		if err := json.Unmarshal(res.ResponseBody, payment); err != nil {
			return fmt.Errorf("failed unmarshaling payment: %w", err)
		}
		return nil
	})

	if execErr != nil {
		return nil, false, execErr
	}
	return payment, isReplay, nil
}

func (s *PaymentService) CapturePayment(ctx context.Context, merchantID, paymentID uuid.UUID, captureAmount *int64, idemKey, requestHash string) (*domain.Payment, bool, error) {
	var payment *domain.Payment
	var isReplay bool

	execErr := s.txManager.WithTx(ctx, func(tx repository.DBTX) error {
		res, err := s.idempotencySvc.Execute(
			ctx, tx, merchantID, idemKey, requestHash,
			func() (int, any, *uuid.UUID, error) {
				// 1. Acquire row-level lock on payment to serialize concurrent capture/refund requests
				p, err := s.paymentRepo.GetByIDForUpdate(ctx, tx, merchantID, paymentID)
				if err != nil {
					return 0, nil, nil, err
				}

				// 2. Validate capture amount & state transition
				toCapture := p.Amount
				if captureAmount != nil && *captureAmount > 0 {
					toCapture = *captureAmount
				}

				if err := p.ValidateCapture(toCapture); err != nil {
					return 0, nil, nil, err
				}

				p.Status = domain.PaymentStatusCaptured
				p.CapturedAmount = toCapture

				// 3. Update payment record
				if err := s.paymentRepo.Update(ctx, tx, p); err != nil {
					return 0, nil, nil, err
				}

				// 4. Atomically create balanced double-entry ledger transaction
				ledgerTx, err := s.ledgerService.PostPaymentCapture(ctx, tx, p, s.platformFeeBps, s.platformFeeFix)
				if err != nil {
					return 0, nil, nil, fmt.Errorf("failed posting capture ledger transaction: %w", err)
				}

				// 5. Atomically insert outbox events (payment.captured + ledger.posted)
				now := time.Now()
				paymentPayload, _ := json.Marshal(p)
				if err := s.outboxRepo.Create(ctx, tx, &domain.OutboxEvent{
					ID:            uuid.New(),
					EventType:     domain.EventPaymentCaptured,
					AggregateType: "payment",
					AggregateID:   p.ID,
					Payload:       paymentPayload,
					Status:        domain.OutboxStatusPending,
					NextRetryAt:   now,
					CreatedAt:     now,
				}); err != nil {
					return 0, nil, nil, err
				}

				ledgerPayload, _ := json.Marshal(ledgerTx)
				if err := s.outboxRepo.Create(ctx, tx, &domain.OutboxEvent{
					ID:            uuid.New(),
					EventType:     domain.EventLedgerPosted,
					AggregateType: "ledger",
					AggregateID:   ledgerTx.ID,
					Payload:       ledgerPayload,
					Status:        domain.OutboxStatusPending,
					NextRetryAt:   now,
					CreatedAt:     now,
				}); err != nil {
					return 0, nil, nil, err
				}

				return 200, p, &p.ID, nil
			},
		)
		if err != nil {
			return err
		}

		isReplay = res.IsReplay
		payment = &domain.Payment{}
		if err := json.Unmarshal(res.ResponseBody, payment); err != nil {
			return fmt.Errorf("failed unmarshaling payment: %w", err)
		}
		return nil
	})

	if execErr != nil {
		return nil, false, execErr
	}
	return payment, isReplay, nil
}

func (s *PaymentService) CancelPayment(ctx context.Context, merchantID, paymentID uuid.UUID, idemKey, requestHash string) (*domain.Payment, bool, error) {
	var payment *domain.Payment
	var isReplay bool

	execErr := s.txManager.WithTx(ctx, func(tx repository.DBTX) error {
		res, err := s.idempotencySvc.Execute(
			ctx, tx, merchantID, idemKey, requestHash,
			func() (int, any, *uuid.UUID, error) {
				p, err := s.paymentRepo.GetByIDForUpdate(ctx, tx, merchantID, paymentID)
				if err != nil {
					return 0, nil, nil, err
				}

				if !p.CanTransitionTo(domain.PaymentStatusCanceled) {
					return 0, nil, nil, fmt.Errorf("%w: cannot cancel payment in '%s' status", domain.ErrInvalidStateTransition, p.Status)
				}

				p.Status = domain.PaymentStatusCanceled
				if err := s.paymentRepo.Update(ctx, tx, p); err != nil {
					return 0, nil, nil, err
				}

				now := time.Now()
				payload, _ := json.Marshal(p)
				if err := s.outboxRepo.Create(ctx, tx, &domain.OutboxEvent{
					ID:            uuid.New(),
					EventType:     domain.EventPaymentCanceled,
					AggregateType: "payment",
					AggregateID:   p.ID,
					Payload:       payload,
					Status:        domain.OutboxStatusPending,
					NextRetryAt:   now,
					CreatedAt:     now,
				}); err != nil {
					return 0, nil, nil, err
				}

				return 200, p, &p.ID, nil
			},
		)
		if err != nil {
			return err
		}

		isReplay = res.IsReplay
		payment = &domain.Payment{}
		if err := json.Unmarshal(res.ResponseBody, payment); err != nil {
			return fmt.Errorf("failed unmarshaling payment: %w", err)
		}
		return nil
	})

	if execErr != nil {
		return nil, false, execErr
	}
	return payment, isReplay, nil
}

func (s *PaymentService) GetPayment(ctx context.Context, merchantID, paymentID uuid.UUID) (*domain.Payment, error) {
	var payment *domain.Payment
	err := s.txManager.WithTx(ctx, func(tx repository.DBTX) error {
		var err error
		payment, err = s.paymentRepo.GetByID(ctx, tx, merchantID, paymentID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return payment, nil
}

func (s *PaymentService) ListPayments(ctx context.Context, filter repository.ListPaymentsFilter) ([]domain.Payment, *time.Time, error) {
	var payments []domain.Payment
	var nextCursor *time.Time
	err := s.txManager.WithTx(ctx, func(tx repository.DBTX) error {
		var err error
		payments, nextCursor, err = s.paymentRepo.List(ctx, tx, filter)
		return err
	})
	if err != nil {
		return nil, nil, err
	}
	return payments, nextCursor, nil
}
