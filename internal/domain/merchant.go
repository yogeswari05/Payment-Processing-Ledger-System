package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/google/uuid"
)

type MerchantStatus string

const (
	MerchantStatusActive    MerchantStatus = "active"
	MerchantStatusSuspended MerchantStatus = "suspended"
	MerchantStatusClosed    MerchantStatus = "closed"
)

// Merchant represents an authenticated business entity processing payments.
type Merchant struct {
	ID         uuid.UUID      `json:"id"`
	Name       string         `json:"name"`
	Status     MerchantStatus `json:"status"`
	APIKeyHash string         `json:"-"`
	CreatedAt  time.Time      `json:"created_at"`
	UpdatedAt  time.Time      `json:"updated_at"`
}

// HashAPIKey returns the SHA-256 hex string of a raw API key.
func HashAPIKey(rawKey string) string {
	sum := sha256.Sum256([]byte(rawKey))
	return hex.EncodeToString(sum[:])
}

// Customer represents an end-customer belonging to a merchant.
type Customer struct {
	ID         uuid.UUID `json:"id"`
	MerchantID uuid.UUID `json:"merchant_id"`
	Email      string    `json:"email"`
	Name       string    `json:"name"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}
