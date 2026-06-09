package orders

import (
	"strings"
	"time"
)

// ExternalOrderID generates a client order ID following the format:
// SYMBOL (alphanumeric only) + SETTLETIME (alphanumeric DDMMYYYYHHmmss in GMT+7) + "_" + EXCHANGE.
// The entire string is converted to upper case and truncated to a maximum of 32 characters.
func ExternalOrderID(symbol string, settleTime time.Time, exchange string) string {
	// 1. Filter symbol: alphanumeric characters only
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

	// 3. Concatenate: symbol + settletime_ + exchange (note the trailing underscore for settletime)
	rawID := symFiltered + settleStr + exchange

	// 4. Upper case the whole string
	upperID := strings.ToUpper(rawID)

	// 5. Truncate based on exchange limit (Gate.io has a 30-character limit for the client order ID,
	// so with the "t-" prefix, the external ID is limited to 28 characters).
	maxLen := 32
	if strings.EqualFold(exchange, "gate") {
		maxLen = 28
	}

	if len(upperID) > maxLen {
		return upperID[:maxLen]
	}
	return upperID
}
