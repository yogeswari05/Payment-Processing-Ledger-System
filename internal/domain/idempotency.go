package domain

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type IdempotencyStatus string

const (
	IdempotencyStatusStarted   IdempotencyStatus = "started"
	IdempotencyStatusCompleted IdempotencyStatus = "completed"
)

// IdempotencyRecord stores the request fingerprint and saved response.
type IdempotencyRecord struct {
	MerchantID   uuid.UUID         `json:"merchant_id"`
	Key          string            `json:"key"`
	RequestHash  string            `json:"request_hash"`
	Status       IdempotencyStatus `json:"status"`
	ResourceID   *uuid.UUID        `json:"resource_id,omitempty"`
	ResponseCode int               `json:"response_code,omitempty"`
	ResponseBody json.RawMessage   `json:"response_body,omitempty"`
	CreatedAt    time.Time         `json:"created_at"`
	ExpiresAt    time.Time         `json:"expires_at"`
}

// ComputeRequestHash generates a deterministic SHA-256 hash from HTTP method, path, and normalized body.
func ComputeRequestHash(method, path string, body []byte) string {
	var normalized []byte
	if len(body) > 0 {
		var compact bytes.Buffer
		if err := json.Compact(&compact, body); err == nil {
			normalized = compact.Bytes()
		} else {
			normalized = body
		}
	}

	payload := fmt.Sprintf("%s:%s:%s", method, path, string(normalized))
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])
}
