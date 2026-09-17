package idutil_test

import (
	"regexp"
	"testing"

	"crypto-bot/pkg/idutil"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNanoID_DefaultAlphabet(t *testing.T) {
	t.Parallel()

	alphabetRegex := regexp.MustCompile(`^[a-zA-Z0-9]{20}$`)

	for range 100 {
		id := idutil.NanoID(20)
		assert.Len(t, id, 20)
		assert.True(t, alphabetRegex.MatchString(id), "NanoID should only contain letters and digits (a-z, A-Z, 0-9), got: %s", id)
	}
}

func TestNanoID_Uniqueness(t *testing.T) {
	t.Parallel()

	seen := make(map[string]struct{}, 10000)
	for range 10000 {
		id := idutil.NanoID(20)
		_, exists := seen[id]
		require.False(t, exists, "collision detected for NanoID: %s", id)
		seen[id] = struct{}{}
	}
}

func TestNanoID_EdgeCases(t *testing.T) {
	t.Parallel()

	assert.Empty(t, idutil.NanoID(0))
	assert.Empty(t, idutil.NanoID(-5))
	assert.Empty(t, idutil.NanoIDWithAlphabet(10, ""))
}

func TestNanoIDWithAlphabet_Custom(t *testing.T) {
	t.Parallel()

	customAlphabet := "ABC"
	customRegex := regexp.MustCompile(`^[ABC]{15}$`)

	for range 50 {
		id := idutil.NanoIDWithAlphabet(15, customAlphabet)
		assert.Len(t, id, 15)
		assert.True(t, customRegex.MatchString(id))
	}
}
