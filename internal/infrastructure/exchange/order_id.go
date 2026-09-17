package exchange

import (
	"crypto-bot/pkg/formatutil"
	"crypto-bot/pkg/idutil"
	"strings"
	"time"
)

// GenerateClientOrderID generates a random alphanumeric client order ID up to the maximum allowed length for the exchange.
func GenerateClientOrderID(exchange string) string {
	maxLen := MaxClientOrderIDLength(exchange)
	return idutil.NanoID(maxLen)
}

// ExternalUniqueID generates a client order ID following the format:
// SYMBOL (alphanumeric only) + SETTLETIME (alphanumeric DDMMYYYYHHmmss in GMT+7) + "_" + EXCHANGE.
// The entire string is converted to upper case and truncated to a maximum of 32 characters.
func ExternalUniqueID(symbol string, settleTime time.Time, exchange string) string {
	symFiltered := formatutil.GetNormalizedSymbol(symbol)

	// 2. Format settle time in GMT+7 time zone directly as alphanumeric DDMMYYYYHHmmss
	loc := time.FixedZone("GMT+7", 7*60*60)
	settleLocal := settleTime.In(loc)
	settleStr := settleLocal.Format("02012006150405")
	exchange = strings.ReplaceAll(exchange, "_", "")

	// 3. Concatenate: settleStr + symFiltered + exchange
	rawID := settleStr + symFiltered + exchange

	// 4. Upper case the whole string
	return strings.ToUpper(rawID)
}

// MaxClientOrderIDLength returns the maximum allowed length for a client order ID on the specified exchange.
func MaxClientOrderIDLength(exchange string) int {
	switch {
	case strings.Contains(strings.ToLower(exchange), "deepcoin"):
		return 20
	case strings.Contains(strings.ToLower(exchange), "gate"):
		return 28
	case strings.Contains(strings.ToLower(exchange), "orangex"):
		return 30
	default:
		return 32
	}
}

// GetMaxClientOrderIDLength is an alias for MaxClientOrderIDLength.
func GetMaxClientOrderIDLength(exchange string) int {
	return MaxClientOrderIDLength(exchange)
}

// ExternalOrderID truncates and returns the generated client order ID.
func ExternalOrderID(symbol string, settleTime time.Time, exchange string) string {
	upperID := ExternalUniqueID(symbol, settleTime, exchange)
	maxLen := MaxClientOrderIDLength(exchange)

	if len(upperID) > maxLen {
		return upperID[:maxLen]
	}
	return upperID
}
