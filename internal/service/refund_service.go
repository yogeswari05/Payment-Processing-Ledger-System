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

type RefundService struct {
	txManager      repository.TxManager
	paymentRepo    repository.PaymentRepo
	refundRepo     repository.RefundRepo
	ledgerService  *LedgerService
	outboxRepo     repository.OutboxRepo
	idempotencySvc *IdempotencyService
}

func NewRefundService(
	tm repository.TxManager,
	pr repository.PaymentRepo,
	rr repository.RefundRepo,
	ls *LedgerService,
	or repository.OutboxRepo,
	is *IdempotencyService,
) *RefundService {
	return &RefundService{
		txManager:      tm,
		paymentRepo:    pr,
		refundRepo:     rr,
		ledgerService:  ls,
		outboxRepo:     or,
		idempotencySvc: is,
	}
}

type CreateRefundParams struct {
	MerchantID     uuid.UUID
	PaymentID      uuid.UUID
	Amount         int64
	Reason         string
	IdempotencyKey string
	RequestHash    string
}

func (s *RefundService) CreateRefund(ctx context.Context, params CreateRefundParams) (*domain.Refund, bool, error) {
	if params.Amount <= 0 {
		return nil, false, domain.ErrZeroRefundAmount
	}

	var refund *domain.Refund
	var isReplay bool

	execErr := s.txManager.WithTx(ctx, func(tx repository.DBTX) error {
		res, err := s.idempotencySvc.Execute(
			ctx, tx, params.MerchantID, params.IdempotencyKey, params.RequestHash,
			func() (int, any, *uuid.UUID, error) {
				// 1. Acquire row-level lock on payment to prevent concurrent over-refunding
				p, err := s.paymentRepo.GetByIDForUpdate(ctx, tx, params.MerchantID, params.PaymentID)
				if err != nil {
					return 0, nil, nil, err
				}

				// 2. Validate refund against payment status and available refundable amount
				if err := p.ValidateRefund(params.Amount); err != nil {
					return 0, nil, nil, err
				}

				now := time.Now()
				refID := uuid.New()

				r := &domain.Refund{
					ID:         refID,
					PaymentID:  p.ID,
					MerchantID: p.MerchantID,
					Amount:     params.Amount,
					Currency:   p.Currency,
					Status:     domain.RefundStatusSucceeded,
					Reason:     params.Reason,
					CreatedAt:  now,
					UpdatedAt:  now,
				}

				// 3. Persist refund record
				if err := s.refundRepo.Create(ctx, tx, r); err != nil {
					return 0, nil, nil, fmt.Errorf("failed creating refund record: %w", err)
				}

				// 4. Update payment's refunded_amount and status
				p.RefundedAmount += params.Amount
				if p.RefundedAmount == p.CapturedAmount {
					p.Status = domain.PaymentStatusRefunded
				} else {
					p.Status = domain.PaymentStatusPartiallyRefunded
				}

				if err := s.paymentRepo.Update(ctx, tx, p); err != nil {
					return 0, nil, nil, fmt.Errorf("failed updating payment on refund: %w", err)
				}

				// 5. Create balanced double-entry ledger postings for the refund
				ledgerTx, err := s.ledgerService.PostPaymentRefund(ctx, tx, r)
				if err != nil {
					return 0, nil, nil, fmt.Errorf("failed posting refund ledger transaction: %w", err)
				}

				// 6. Atomically persist outbox events
				refundPayload, _ := json.Marshal(r)
				if err := s.outboxRepo.Create(ctx, tx, &domain.OutboxEvent{
					ID:            uuid.New(),
					EventType:     domain.EventPaymentRefunded,
					AggregateType: "refund",
					AggregateID:   r.ID,
					Payload:       refundPayload,
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

				return 201, r, &r.ID, nil
			},
		)
		if err != nil {
			return err
		}

		isReplay = res.IsReplay
		refund = &domain.Refund{}
		if err := json.Unmarshal(res.ResponseBody, refund); err != nil {
			return fmt.Errorf("failed unmarshaling stored refund response: %w", err)
		}
		return nil
	})

	if execErr != nil {
		return nil, false, execErr
	}
	return refund, isReplay, nil
}

func (s *RefundService) GetRefund(ctx context.Context, merchantID, refundID uuid.UUID) (*domain.Refund, error) {
	var ref *domain.Refund
	err := s.txManager.WithTx(ctx, func(tx repository.DBTX) error {
		var err error
		ref, err = s.refundRepo.GetByID(ctx, tx, merchantID, refundID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return ref, nil
}
