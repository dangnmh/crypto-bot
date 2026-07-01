package exchange

import (
	"strings"
	"time"
)

// ExternalUniqueID generates a client order ID following the format:
// SYMBOL (alphanumeric only) + SETTLETIME (alphanumeric DDMMYYYYHHmmss in GMT+7) + "_" + EXCHANGE.
// The entire string is converted to upper case and truncated to a maximum of 32 characters.
func ExternalUniqueID(symbol string, settleTime time.Time, exchange string) string {
	var sb strings.Builder
	for _, r := range symbol {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			sb.WriteRune(r)
		}
	}
	symFiltered := sb.String()

	// 2. Format settle time in GMT+7 time zone directly as alphanumeric DDMMYYYYHHmmss
	loc := time.FixedZone("GMT+7", 7*60*60)
	settleLocal := settleTime.In(loc)
	settleStr := settleLocal.Format("02012006150405")

	// 3. Concatenate: settleStr + exchange + symFiltered
	rawID := settleStr + exchange + symFiltered

	// 4. Upper case the whole string
	return strings.ToUpper(rawID)
}

// ExternalOrderID truncates and returns the generated client order ID.
func ExternalOrderID(symbol string, settleTime time.Time, exchange string) string {
	upperID := ExternalUniqueID(symbol, settleTime, exchange)
	maxLen := 32
	if strings.EqualFold(exchange, "gate") {
		maxLen = 28
	} else if strings.EqualFold(exchange, "orangex") {
		maxLen = 30
	}

	if len(upperID) > maxLen {
		return upperID[:maxLen]
	}
	return upperID
}
