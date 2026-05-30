package types

import (
	"encoding/json"
	"errors"
	"time"
)

// Duration wraps time.Duration to provide JSON unmarshaling from strings like "30s", "5m".
type Duration time.Duration

// UnmarshalJSON parses a duration string or falls back to number.
func (d *Duration) UnmarshalJSON(b []byte) error {
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	switch value := v.(type) {
	case float64:
		*d = Duration(time.Duration(value))
		return nil
	case string:
		tmp, err := time.ParseDuration(value)
		if err != nil {
			return err
		}
		*d = Duration(tmp)
		return nil
	default:
		return errors.New("invalid duration")
	}
}
