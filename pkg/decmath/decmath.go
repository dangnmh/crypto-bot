// Package decmath provides precision-safe math utilities for financial calculations.
// It wraps shopspring/decimal to eliminate float64 rounding errors in price/volume
// arithmetic critical for exchange order placement.
//
// Usage: Replace raw float64 math with these functions at calculation boundaries
// (price snapping, volume rounding, slippage) where precision matters most.
package decmath

import (
	"fmt"

	"github.com/shopspring/decimal"
)

// Decimal is the canonical high-precision number type for financial values.
type Decimal = decimal.Decimal

// FromFloat converts a float64 boundary value into Decimal.
func FromFloat(v float64) Decimal {
	return decimal.NewFromFloat(v)
}

// FromString parses a decimal string.
func FromString(v string) (Decimal, error) {
	d, err := decimal.NewFromString(v)
	if err != nil {
		return decimal.Zero, fmt.Errorf("parse decimal %q: %w", v, err)
	}
	return d, nil
}

// MustFromString parses a decimal string and panics on invalid input.
// Use only in tests and static constants.
func MustFromString(v string) Decimal {
	d, err := FromString(v)
	if err != nil {
		panic(err)
	}
	return d
}

// ToFloat converts Decimal back to float64 at external API boundaries.
func ToFloat(v Decimal) float64 {
	return v.InexactFloat64()
}

// ──────────────────────────────────────────────────────────────────────
// Rounding & Snapping
// ──────────────────────────────────────────────────────────────────────.

// RoundDecimalToScale rounds v to n decimal places.
func RoundDecimalToScale(v Decimal, scale int) Decimal {
	return v.Round(int32(scale))
}

// RoundToScale rounds v to n decimal places using banker's rounding (round half-even).
// Replaces: math.Round(v * pow10(n)) / pow10(n).
func RoundToScale(v float64, scale int) float64 {
	return ToFloat(RoundDecimalToScale(FromFloat(v), scale))
}

// FloorDecimalToScale truncates v down to n decimal places.
func FloorDecimalToScale(v Decimal, scale int) Decimal {
	p := decimal.New(1, int32(scale)) // 10^scale
	return v.Mul(p).Floor().Div(p)
}

// FloorToScale truncates v down to n decimal places.
// Replaces: math.Floor(v * pow10(n)) / pow10(n).
func FloorToScale(v float64, scale int) float64 {
	return ToFloat(FloorDecimalToScale(FromFloat(v), scale))
}

// CeilDecimalToScale rounds v up to n decimal places.
func CeilDecimalToScale(v Decimal, scale int) Decimal {
	p := decimal.New(1, int32(scale))
	return v.Mul(p).Ceil().Div(p)
}

// CeilToScale rounds v up to n decimal places.
func CeilToScale(v float64, scale int) float64 {
	return ToFloat(CeilDecimalToScale(FromFloat(v), scale))
}

// ──────────────────────────────────────────────────────────────────────
// Tick-aligned price operations
// ──────────────────────────────────────────────────────────────────────.

// SnapDecimalToTickFloor snaps a price DOWN to the nearest tick.
func SnapDecimalToTickFloor(price, tick Decimal) Decimal {
	if tick.IsZero() {
		return price
	}
	return price.Div(tick).Floor().Mul(tick)
}

// SnapToTickFloor snaps a price DOWN to the nearest tick (priceUnit).
// E.g., SnapToTickFloor(100.37, 0.05) → 100.35.
func SnapToTickFloor(price, tick float64) float64 {
	return ToFloat(SnapDecimalToTickFloor(FromFloat(price), FromFloat(tick)))
}

// SnapDecimalToTickCeil snaps a price UP to the nearest tick.
func SnapDecimalToTickCeil(price, tick Decimal) Decimal {
	if tick.IsZero() {
		return price
	}
	return price.Div(tick).Ceil().Mul(tick)
}

// SnapToTickCeil snaps a price UP to the nearest tick (priceUnit).
// E.g., SnapToTickCeil(100.31, 0.05) → 100.35.
func SnapToTickCeil(price, tick float64) float64 {
	return ToFloat(SnapDecimalToTickCeil(FromFloat(price), FromFloat(tick)))
}

// ──────────────────────────────────────────────────────────────────────
// Arithmetic helpers
// ──────────────────────────────────────────────────────────────────────.

func MulDecimal(a, b Decimal) Decimal {
	return a.Mul(b)
}

// Mul returns a * b with full decimal precision, truncated back to float64.
func Mul(a, b float64) float64 {
	return ToFloat(MulDecimal(FromFloat(a), FromFloat(b)))
}

func DivDecimal(a, b Decimal) Decimal {
	if b.IsZero() {
		return decimal.Zero
	}
	return a.Div(b)
}

// Div returns a / b with full decimal precision. Returns 0 if b is zero.
func Div(a, b float64) float64 {
	return ToFloat(DivDecimal(FromFloat(a), FromFloat(b)))
}

func AddDecimal(a, b Decimal) Decimal {
	return a.Add(b)
}

// Add returns a + b with full decimal precision.
func Add(a, b float64) float64 {
	return ToFloat(AddDecimal(FromFloat(a), FromFloat(b)))
}

func SubDecimal(a, b Decimal) Decimal {
	return a.Sub(b)
}

// Sub returns a - b with full decimal precision.
func Sub(a, b float64) float64 {
	return ToFloat(SubDecimal(FromFloat(a), FromFloat(b)))
}

// ──────────────────────────────────────────────────────────────────────
// Comparison helpers
// ──────────────────────────────────────────────────────────────────────.

func EqualDecimal(a, b Decimal) bool {
	return a.Equal(b)
}

// Equal returns true if a and b are equal in decimal representation.
func Equal(a, b float64) bool {
	return EqualDecimal(FromFloat(a), FromFloat(b))
}

func GreaterThanDecimal(a, b Decimal) bool {
	return a.GreaterThan(b)
}

// GreaterThan returns true if a > b in decimal representation.
func GreaterThan(a, b float64) bool {
	return GreaterThanDecimal(FromFloat(a), FromFloat(b))
}

func LessThanDecimal(a, b Decimal) bool {
	return a.LessThan(b)
}

// LessThan returns true if a < b in decimal representation.
func LessThan(a, b float64) bool {
	return LessThanDecimal(FromFloat(a), FromFloat(b))
}

// RatioToPercent converts a ratio such as 0.005 to percentage 0.5.
func RatioToPercent(v Decimal) Decimal {
	return v.Mul(decimal.NewFromInt(100))
}

// PercentToRatio converts a percentage such as 0.5 to ratio 0.005.
func PercentToRatio(v Decimal) Decimal {
	return v.Div(decimal.NewFromInt(100))
}

// ──────────────────────────────────────────────────────────────────────
// Format
// ──────────────────────────────────────────────────────────────────────.

// FormatPrice formats a price to exactly n decimal places as a string.
// Useful for logging and display. E.g., FormatPrice(0.005, 4) → "0.0050".
func FormatPrice(price float64, scale int) string {
	return FormatDecimal(FromFloat(price), scale)
}

// FormatDecimal formats a Decimal to exactly n decimal places as a string.
func FormatDecimal(price Decimal, scale int) string {
	return price.StringFixed(int32(scale))
}

// SignedTradingFee ensures a trading fee is represented as a negative value.
func SignedTradingFee(value float64) float64 {
	if value > 0 {
		return -value
	}
	return value
}

// ClosedNetProfit calculates net profit by adding the signed trading fee.
func ClosedNetProfit(profit, fee float64) float64 {
	return Add(profit, SignedTradingFee(fee))
}

// CalcSpreadPct calculates the spread percentage between bid and ask.
func CalcSpreadPct(bestBid, bestAsk float64) float64 {
	if bestBid <= 0 {
		return 0
	}
	return Mul(Div(Sub(bestAsk, bestBid), bestBid), 100.0)
}
