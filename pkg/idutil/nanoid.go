package idutil

import (
	"crypto/rand"
	"math/big"
)

// Alphabet defines English letters and digits (a-z, A-Z, 0-9).
const Alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

// NanoID generates a cryptographically secure random string of the specified size
// using English alphanumeric characters (a-z, A-Z, 0-9).
func NanoID(size int) string {
	return NanoIDWithAlphabet(size, Alphabet)
}

// NanoIDWithAlphabet generates a cryptographically secure random string of the specified size
// using the provided alphabet. If size <= 0 or alphabet is empty, it returns an empty string.
func NanoIDWithAlphabet(size int, alphabet string) string {
	if size <= 0 || alphabet == "" {
		return ""
	}

	bytes := make([]byte, size)
	limit := big.NewInt(int64(len(alphabet)))
	for i := range size {
		num, err := rand.Int(rand.Reader, limit)
		if err != nil {
			panic(err)
		}
		bytes[i] = alphabet[num.Int64()]
	}
	return string(bytes)
}
