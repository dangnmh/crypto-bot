// Package decmath provides precision-safe math utilities for financial calculations.
// It wraps shopspring/decimal to eliminate float64 rounding errors in price/volume
// arithmetic critical for exchange order placement.
//
// Usage: Replace raw float64 math with these functions at calculation boundaries
// (price snapping, volume rounding, slippage) where precision matters most.
package decmath

import (
	"github.com/shopspring/decimal"
)

// ──────────────────────────────────────────────────────────────────────
// Rounding & Snapping
// ──────────────────────────────────────────────────────────────────────.

// RoundToScale rounds v to n decimal places using banker's rounding (round half-even).
// Replaces: math.Round(v * pow10(n)) / pow10(n).
func RoundToScale(v float64, scale int) float64 {
	d := decimal.NewFromFloat(v)
	return d.Round(int32(scale)).InexactFloat64()
}

// FloorToScale truncates v down to n decimal places.
// Replaces: math.Floor(v * pow10(n)) / pow10(n).
func FloorToScale(v float64, scale int) float64 {
	d := decimal.NewFromFloat(v)
	p := decimal.New(1, int32(scale)) // 10^scale
	return d.Mul(p).Floor().Div(p).InexactFloat64()
}

// CeilToScale rounds v up to n decimal places.
func CeilToScale(v float64, scale int) float64 {
	d := decimal.NewFromFloat(v)
	p := decimal.New(1, int32(scale))
	return d.Mul(p).Ceil().Div(p).InexactFloat64()
}

// ──────────────────────────────────────────────────────────────────────
// Tick-aligned price operations
// ──────────────────────────────────────────────────────────────────────.

// SnapToTickFloor snaps a price DOWN to the nearest tick (priceUnit).
// E.g., SnapToTickFloor(100.37, 0.05) → 100.35.
func SnapToTickFloor(price, tick float64) float64 {
	dp := decimal.NewFromFloat(price)
	dt := decimal.NewFromFloat(tick)
	if dt.IsZero() {
		return price
	}
	// floor(price / tick) * tick
	return dp.Div(dt).Floor().Mul(dt).InexactFloat64()
}

// SnapToTickCeil snaps a price UP to the nearest tick (priceUnit).
// E.g., SnapToTickCeil(100.31, 0.05) → 100.35.
func SnapToTickCeil(price, tick float64) float64 {
	dp := decimal.NewFromFloat(price)
	dt := decimal.NewFromFloat(tick)
	if dt.IsZero() {
		return price
	}
	// ceil(price / tick) * tick
	return dp.Div(dt).Ceil().Mul(dt).InexactFloat64()
}

// ──────────────────────────────────────────────────────────────────────
// Arithmetic helpers
// ──────────────────────────────────────────────────────────────────────.

// Mul returns a * b with full decimal precision, truncated back to float64.
func Mul(a, b float64) float64 {
	return decimal.NewFromFloat(a).Mul(decimal.NewFromFloat(b)).InexactFloat64()
}

// Div returns a / b with full decimal precision. Returns 0 if b is zero.
func Div(a, b float64) float64 {
	db := decimal.NewFromFloat(b)
	if db.IsZero() {
		return 0
	}
	return decimal.NewFromFloat(a).Div(db).InexactFloat64()
}

// Add returns a + b with full decimal precision.
func Add(a, b float64) float64 {
	return decimal.NewFromFloat(a).Add(decimal.NewFromFloat(b)).InexactFloat64()
}

// Sub returns a - b with full decimal precision.
func Sub(a, b float64) float64 {
	return decimal.NewFromFloat(a).Sub(decimal.NewFromFloat(b)).InexactFloat64()
}

// ──────────────────────────────────────────────────────────────────────
// Comparison helpers
// ──────────────────────────────────────────────────────────────────────.

// Equal returns true if a and b are equal in decimal representation.
func Equal(a, b float64) bool {
	return decimal.NewFromFloat(a).Equal(decimal.NewFromFloat(b))
}

// GreaterThan returns true if a > b in decimal representation.
func GreaterThan(a, b float64) bool {
	return decimal.NewFromFloat(a).GreaterThan(decimal.NewFromFloat(b))
}

// LessThan returns true if a < b in decimal representation.
func LessThan(a, b float64) bool {
	return decimal.NewFromFloat(a).LessThan(decimal.NewFromFloat(b))
}

// ──────────────────────────────────────────────────────────────────────
// Format
// ──────────────────────────────────────────────────────────────────────.

// FormatPrice formats a price to exactly n decimal places as a string.
// Useful for logging and display. E.g., FormatPrice(0.005, 4) → "0.0050".
func FormatPrice(price float64, scale int) string {
	return decimal.NewFromFloat(price).StringFixed(int32(scale))
}
