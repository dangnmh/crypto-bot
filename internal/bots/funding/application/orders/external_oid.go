package orders

import (
	"hash/fnv"
	"strconv"
	"time"
)

const maxExternalOrderIDLen = 30

func ExternalOrderID(prefix, symbol string) string {
	return externalOrderID(prefix, symbol, time.Now())
}

func externalOrderID(prefix, symbol string, now time.Time) string {
	symbolHash := fnv.New32a()
	_, _ = symbolHash.Write([]byte(symbol))

	id := prefix + "_" +
		strconv.FormatUint(uint64(symbolHash.Sum32()), 36) + "_" +
		strconv.FormatInt(now.UnixMilli(), 36)
	if len(id) <= maxExternalOrderIDLen {
		return id
	}
	return id[:maxExternalOrderIDLen]
}
