package formatutil

import (
	"fmt"
	"math"
	"strings"
)

// FormatPrice formats a float64 price to a 6-decimal string.
func FormatPrice(price float64) string {
	return fmt.Sprintf("%.6f", price)
}

// FormatPriceWithCommas formats a float64 price with thousand separators and 6 decimals.
func FormatPriceWithCommas(price float64) string {
	parts := strings.Split(FormatPrice(price), ".")
	intPart := parts[0]
	decPart := ""
	if len(parts) > 1 {
		decPart = "." + parts[1]
	}

	isNegative := strings.HasPrefix(intPart, "-")
	if isNegative {
		intPart = intPart[1:]
	}

	var result []string
	length := len(intPart)
	for i := length; i > 0; i -= 3 {
		start := max(i-3, 0)
		result = append([]string{intPart[start:i]}, result...)
	}

	prefix := ""
	if isNegative {
		prefix = "-"
	}

	return prefix + strings.Join(result, ",") + decPart
}

// FormatUSDWithCommasAndDecimals formats a float64 value with thousand separators and a custom number of decimal places.
func FormatUSDWithCommasAndDecimals(val float64, decimals int) string {
	parts := strings.Split(fmt.Sprintf("%.*f", decimals, val), ".")
	intPart := parts[0]
	decPart := ""
	if len(parts) > 1 {
		decPart = "." + parts[1]
	}

	isNegative := strings.HasPrefix(intPart, "-")
	if isNegative {
		intPart = intPart[1:]
	}

	var result []string
	length := len(intPart)
	for i := length; i > 0; i -= 3 {
		start := max(i-3, 0)
		result = append([]string{intPart[start:i]}, result...)
	}

	prefix := ""
	if isNegative {
		prefix = "-"
	}

	return prefix + strings.Join(result, ",") + decPart
}

// FormatUSDWithCommas formats a float64 USD value with thousand separators and 2 decimal places.
func FormatUSDWithCommas(val float64) string {
	return FormatUSDWithCommasAndDecimals(val, 2)
}

// FormatNetPnL formats a net PnL and percentage into a standard signed dollar and percentage string with 4 decimal places.
func FormatNetPnL(netPnL, pnlPct float64) string {
	signNet := ""
	if netPnL > 0 {
		signNet = "+"
	}
	signPct := ""
	if pnlPct > 0 {
		signPct = "+"
	}

	netStr := FormatUSDWithCommasAndDecimals(math.Abs(netPnL), 4)

	if netPnL < 0 {
		return fmt.Sprintf("-$%s (%.2f%%)", netStr, pnlPct)
	}
	return fmt.Sprintf("%s$%s (%s%.2f%%)", signNet, netStr, signPct, pnlPct)
}

// FormatFundingFee formats a funding fee with sign and thousand separators with 4 decimal places.
func FormatFundingFee(val float64) string {
	if val < 0 {
		return fmt.Sprintf("-$%s", FormatUSDWithCommasAndDecimals(-val, 4))
	}
	if val > 0 {
		return fmt.Sprintf("+$%s", FormatUSDWithCommasAndDecimals(val, 4))
	}
	return "$0.0000"
}

// FormatDuration formats milliseconds into a human-readable duration (e.g. "1m 30s" or "45s").
func FormatDuration(ms int64) string {
	if ms <= 0 {
		return "0s"
	}
	sec := ms / 1000
	m := sec / 60
	s := sec % 60
	if m > 0 {
		return fmt.Sprintf("%dm %ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}

// FormatFloatMax4 formats a float64 value to a string with up to 4 decimal places, omitting trailing zeros.
func FormatFloatMax4(val float64) string {
	s := fmt.Sprintf("%.4f", val)
	s = strings.TrimRight(s, "0")
	s = strings.TrimRight(s, ".")
	return s
}
