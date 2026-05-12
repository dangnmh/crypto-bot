package types_test

import (
	"testing"
	"time"

	"crypto-bot/pkg/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDuration_UnmarshalJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    time.Duration
		wantErr bool
	}{
		{
			name:  "string duration seconds",
			input: `"30s"`,
			want:  30 * time.Second,
		},
		{
			name:  "string duration minutes",
			input: `"5m"`,
			want:  5 * time.Minute,
		},
		{
			name:  "string duration milliseconds",
			input: `"100ms"`,
			want:  100 * time.Millisecond,
		},
		{
			name:  "numeric value nanoseconds",
			input: `1000000000`,
			want:  1 * time.Second,
		},
		{
			name:  "numeric zero",
			input: `0`,
			want:  0,
		},
		{
			name:    "invalid string",
			input:   `"not_a_duration"`,
			wantErr: true,
		},
		{
			name:    "boolean value",
			input:   `true`,
			wantErr: true,
		},
		{
			name:    "invalid JSON",
			input:   `{invalid}`,
			wantErr: true,
		},
		{
			name:    "null value",
			input:   `null`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var d types.Duration
			err := d.UnmarshalJSON([]byte(tt.input))

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, time.Duration(d))
		})
	}
}
