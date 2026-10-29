package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/payflow/payflow/internal/domain"
	"github.com/payflow/payflow/internal/repository"
)

type MerchantService struct {
	txManager    repository.TxManager
	merchantRepo repository.MerchantRepo
}

func NewMerchantService(tm repository.TxManager, mr repository.MerchantRepo) *MerchantService {
	return &MerchantService{
		txManager:    tm,
		merchantRepo: mr,
	}
}

// CreateMerchant provisions a new merchant account and generates a secure random API key.
// The raw key is returned only once to the caller; only its SHA-256 hash is persisted.
func (s *MerchantService) CreateMerchant(ctx context.Context, name string) (*domain.Merchant, string, error) {
	if name == "" {
		return nil, "", fmt.Errorf("%w: merchant name cannot be empty", domain.ErrInvalidRequestPayload)
	}

	keyBytes := make([]byte, 24)
	if _, err := rand.Read(keyBytes); err != nil {
		return nil, "", fmt.Errorf("failed generating api key: %w", err)
	}
	rawKey := "sk_test_" + hex.EncodeToString(keyBytes)
	keyHash := domain.HashAPIKey(rawKey)

	merchant := &domain.Merchant{
		ID:         uuid.New(),
		Name:       name,
		Status:     domain.MerchantStatusActive,
		APIKeyHash: keyHash,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}

	err := s.txManager.WithTx(ctx, func(tx repository.DBTX) error {
		return s.merchantRepo.CreateMerchant(ctx, tx, merchant)
	})
	if err != nil {
		return nil, "", fmt.Errorf("failed creating merchant: %w", err)
	}

	return merchant, rawKey, nil
}

// Authenticate verifies the raw API key and returns the active merchant.
func (s *MerchantService) Authenticate(ctx context.Context, rawKey string) (*domain.Merchant, error) {
	if rawKey == "" {
		return nil, domain.ErrUnauthorized
	}

	hash := domain.HashAPIKey(rawKey)
	var merchant *domain.Merchant
	err := s.txManager.WithTx(ctx, func(tx repository.DBTX) error {
		var err error
		merchant, err = s.merchantRepo.GetMerchantByAPIKeyHash(ctx, tx, hash)
		return err
	})
	if err != nil {
		return nil, domain.ErrUnauthorized
	}

	if merchant.Status != domain.MerchantStatusActive {
		return nil, domain.ErrForbidden
	}

	return merchant, nil
}

func (s *MerchantService) CreateCustomer(ctx context.Context, merchantID uuid.UUID, email, name string) (*domain.Customer, error) {
	if email == "" || name == "" {
		return nil, fmt.Errorf("%w: email and name are required", domain.ErrInvalidRequestPayload)
	}

	customer := &domain.Customer{
		ID:         uuid.New(),
		MerchantID: merchantID,
		Email:      email,
		Name:       name,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}

	err := s.txManager.WithTx(ctx, func(tx repository.DBTX) error {
		return s.merchantRepo.CreateCustomer(ctx, tx, customer)
	})
	if err != nil {
		return nil, fmt.Errorf("failed creating customer: %w", err)
	}

	return customer, nil
}

func (s *MerchantService) GetCustomer(ctx context.Context, merchantID, customerID uuid.UUID) (*domain.Customer, error) {
	var customer *domain.Customer
	err := s.txManager.WithTx(ctx, func(tx repository.DBTX) error {
		var err error
		customer, err = s.merchantRepo.GetCustomerByID(ctx, tx, merchantID, customerID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return customer, nil
}
