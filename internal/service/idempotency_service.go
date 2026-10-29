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

type IdempotencyService struct {
	repo        repository.IdempotencyRepo
	expiryHours int
}

func NewIdempotencyService(repo repository.IdempotencyRepo, expiryHours int) *IdempotencyService {
	if expiryHours <= 0 {
		expiryHours = 24
	}
	return &IdempotencyService{
		repo:        repo,
		expiryHours: expiryHours,
	}
}

// ExecutionResult wraps the response status, payload, and whether it was replayed from cache.
type ExecutionResult struct {
	IsReplay     bool
	StatusCode   int
	ResponseBody []byte
}

// Execute performs an operation under strict database-backed idempotency guarantees.
func (s *IdempotencyService) Execute(
	ctx context.Context,
	tx repository.DBTX,
	merchantID uuid.UUID,
	idempotencyKey string,
	requestHash string,
	action func() (int, any, *uuid.UUID, error),
) (*ExecutionResult, error) {
	if idempotencyKey == "" {
		// Non-idempotent execution
		statusCode, data, _, err := action()
		if err != nil {
			return nil, err
		}
		respBytes, err := json.Marshal(data)
		if err != nil {
			return nil, fmt.Errorf("failed marshaling response: %w", err)
		}
		return &ExecutionResult{
			IsReplay:     false,
			StatusCode:   statusCode,
			ResponseBody: respBytes,
		}, nil
	}

	rec := &domain.IdempotencyRecord{
		MerchantID:  merchantID,
		Key:         idempotencyKey,
		RequestHash: requestHash,
		Status:      domain.IdempotencyStatusStarted,
		CreatedAt:   time.Now(),
		ExpiresAt:   time.Now().Add(time.Duration(s.expiryHours) * time.Hour),
	}

	lockedRec, err := s.repo.LockOrGet(ctx, tx, rec)
	if err != nil {
		return nil, fmt.Errorf("failed locking idempotency key: %w", err)
	}

	// Case 1: Already completed request
	if lockedRec.Status == domain.IdempotencyStatusCompleted {
		if lockedRec.RequestHash != requestHash {
			return nil, domain.ErrIdempotencyMismatch
		}
		return &ExecutionResult{
			IsReplay:     true,
			StatusCode:   lockedRec.ResponseCode,
			ResponseBody: []byte(lockedRec.ResponseBody),
		}, nil
	}

	// Case 2: Concurrent request already in progress
	// Note: LockOrGet returns the newly created record with matching CreatedAt if we just created it.
	// If it was created earlier and still in 'started' state, another request holds the lock.
	if lockedRec.Status == domain.IdempotencyStatusStarted && !lockedRec.CreatedAt.Equal(rec.CreatedAt) {
		return nil, domain.ErrIdempotencyKeyConflict
	}

	// Case 3: We have newly claimed the idempotency record. Run business action.
	statusCode, data, resourceID, err := action()
	if err != nil {
		return nil, err
	}

	respBytes, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal response body: %w", err)
	}

	lockedRec.ResourceID = resourceID
	lockedRec.ResponseCode = statusCode
	lockedRec.ResponseBody = json.RawMessage(respBytes)

	if err := s.repo.SaveResponse(ctx, tx, lockedRec); err != nil {
		return nil, fmt.Errorf("failed saving idempotency response: %w", err)
	}

	return &ExecutionResult{
		IsReplay:     false,
		StatusCode:   statusCode,
		ResponseBody: respBytes,
	}, nil
}
