package exchange

import (
	"crypto-bot/pkg/formatutil"
	"strings"
	"time"
)

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

// ExternalOrderID truncates and returns the generated client order ID.
func ExternalOrderID(symbol string, settleTime time.Time, exchange string) string {
	upperID := ExternalUniqueID(symbol, settleTime, exchange)
	maxLen := 32

	switch {
	case strings.Contains(strings.ToLower(exchange), "gate"):
		maxLen = 28
	case strings.Contains(strings.ToLower(exchange), "orangex"):
		maxLen = 30
	case strings.Contains(strings.ToLower(exchange), "deepcoin"):
		maxLen = 20
	}

	if len(upperID) > maxLen {
		return upperID[:maxLen]
	}
	return upperID
}
