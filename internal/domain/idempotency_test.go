package domain_test

import (
	"testing"

	"github.com/payflow/payflow/internal/domain"
	"github.com/stretchr/testify/assert"
)

func TestComputeRequestHash(t *testing.T) {
	t.Run("Deterministic hash for identical inputs", func(t *testing.T) {
		body := []byte(`{"amount": 5000, "currency": "USD"}`)
		hash1 := domain.ComputeRequestHash("POST", "/v1/payments", body)
		hash2 := domain.ComputeRequestHash("POST", "/v1/payments", body)
		assert.Equal(t, hash1, hash2)
		assert.NotEmpty(t, hash1)
	})

	t.Run("JSON whitespace normalization generates identical hash", func(t *testing.T) {
		compactJSON := []byte(`{"amount":5000,"currency":"USD"}`)
		prettyJSON := []byte("{\n  \"amount\": 5000,\n  \"currency\": \"USD\"\n}")
		tabbedJSON := []byte("{\t\"amount\":\t5000,\n\t\"currency\":\t\"USD\"}")

		hashCompact := domain.ComputeRequestHash("POST", "/v1/payments", compactJSON)
		hashPretty := domain.ComputeRequestHash("POST", "/v1/payments", prettyJSON)
		hashTabbed := domain.ComputeRequestHash("POST", "/v1/payments", tabbedJSON)

		assert.Equal(t, hashCompact, hashPretty)
		assert.Equal(t, hashCompact, hashTabbed)
	})

	t.Run("Different payload yields different hash", func(t *testing.T) {
		hashA := domain.ComputeRequestHash("POST", "/v1/payments", []byte(`{"amount": 1000}`))
		hashB := domain.ComputeRequestHash("POST", "/v1/payments", []byte(`{"amount": 2000}`))
		assert.NotEqual(t, hashA, hashB)
	})

	t.Run("Different path yields different hash", func(t *testing.T) {
		body := []byte(`{"amount": 1000}`)
		hash1 := domain.ComputeRequestHash("POST", "/v1/payments", body)
		hash2 := domain.ComputeRequestHash("POST", "/v1/refunds", body)
		assert.NotEqual(t, hash1, hash2)
	})
}
