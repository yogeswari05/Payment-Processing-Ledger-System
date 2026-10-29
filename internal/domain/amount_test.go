package domain_test

import (
	"testing"

	"github.com/payflow/payflow/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateCurrency(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expected    string
		expectError bool
	}{
		{name: "Valid USD lowercase", input: "usd", expected: "USD", expectError: false},
		{name: "Valid EUR with whitespace", input: "  EUR ", expected: "EUR", expectError: false},
		{name: "Valid GBP", input: "GBP", expected: "GBP", expectError: false},
		{name: "Unsupported JPY", input: "JPY", expected: "", expectError: true},
		{name: "Empty currency", input: "", expected: "", expectError: true},
		{name: "Malformed currency", input: "USDD", expected: "", expectError: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			curr, err := domain.ValidateCurrency(tc.input)
			if tc.expectError {
				require.Error(t, err)
				assert.ErrorIs(t, err, domain.ErrUnsupportedCurrency)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tc.expected, curr)
			}
		})
	}
}

func TestValidateAmount(t *testing.T) {
	tests := []struct {
		name        string
		amount      int64
		expectError bool
	}{
		{name: "Standard positive amount 1000 cents ($10.00)", amount: 1000, expectError: false},
		{name: "Minimum positive amount 1 cent", amount: 1, expectError: false},
		{name: "Zero amount", amount: 0, expectError: true},
		{name: "Negative amount", amount: -500, expectError: true},
		{name: "Maximum limit amount", amount: domain.MaxMinorUnitAmount, expectError: false},
		{name: "Exceeds maximum limit", amount: domain.MaxMinorUnitAmount + 1, expectError: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := domain.ValidateAmount(tc.amount)
			if tc.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestFormatMoney(t *testing.T) {
	assert.Equal(t, "10.00 USD", domain.FormatMoney(1000, "USD"))
	assert.Equal(t, "0.50 EUR", domain.FormatMoney(50, "EUR"))
	assert.Equal(t, "123.45 GBP", domain.FormatMoney(12345, "GBP"))
}
