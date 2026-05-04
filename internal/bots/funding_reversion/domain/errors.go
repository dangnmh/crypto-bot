package domain

import "errors"

// Sentinel errors for strategy package.
// Use errors.Is() to check specific error types from callers.
var (
	ErrInvalidSide      = errors.New("invalid side")
	ErrInvalidPriceUnit = errors.New("invalid price unit")
	ErrZeroRefPrice     = errors.New("reference price is zero")
	ErrZeroIOCPrice     = errors.New("calculated IOC price <= 0")
)
