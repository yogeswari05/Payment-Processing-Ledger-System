package domain

import (
	"fmt"
	"strings"
)

// SupportedCurrencies lists the accepted ISO-4217 currency codes.
var SupportedCurrencies = map[string]int{
	"USD": 2, // minor unit: cents (100)
	"EUR": 2, // minor unit: cents (100)
	"GBP": 2, // minor unit: pence (100)
}

// MaxMinorUnitAmount sets a practical safety limit ($10,000,000 in minor units = 1 billion cents)
// to prevent overflow and absurd inputs.
const MaxMinorUnitAmount int64 = 1_000_000_000

// ValidateCurrency ensures the currency is supported.
func ValidateCurrency(currency string) (string, error) {
	upper := strings.ToUpper(strings.TrimSpace(currency))
	if _, ok := SupportedCurrencies[upper]; !ok {
		return "", fmt.Errorf("%w: '%s'", ErrUnsupportedCurrency, currency)
	}
	return upper, nil
}

// ValidateAmount validates that the minor unit amount is positive and within safe limits.
func ValidateAmount(amount int64) error {
	if amount <= 0 {
		return fmt.Errorf("%w: got %d", ErrInvalidAmount, amount)
	}
	if amount > MaxMinorUnitAmount {
		return fmt.Errorf("amount %d exceeds maximum allowed limit of %d", amount, MaxMinorUnitAmount)
	}
	return nil
}

// FormatMoney returns a human-readable display string for logs/diagnostics, e.g. "$10.50 USD".
func FormatMoney(amount int64, currency string) string {
	precision, ok := SupportedCurrencies[currency]
	if !ok {
		precision = 2
	}
	divisor := int64(1)
	for i := 0; i < precision; i++ {
		divisor *= 10
	}
	major := amount / divisor
	minor := amount % divisor
	if minor < 0 {
		minor = -minor
	}
	return fmt.Sprintf("%d.%02d %s", major, minor, currency)
}
