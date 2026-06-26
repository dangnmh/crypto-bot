package reversion

import "errors"

var (
	// ErrFRBelowThreshold is returned when the funding rate dropped below the configured minimum threshold.
	ErrFRBelowThreshold = errors.New("FR below threshold")

	// ErrFRSignFlip is returned when the funding rate flips sign during recheck.
	ErrFRSignFlip = errors.New("FR sign flip")
)
