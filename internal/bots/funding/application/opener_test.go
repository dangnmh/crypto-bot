package application_test

import (
	"errors"
	"testing"

	"crypto-bot/internal/bots/funding/application"

	"github.com/stretchr/testify/assert"
)

func TestOrderResultIsSuccess(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		give application.OrderResult
		want bool
	}{
		{
			name: "Success",
			give: application.OrderResult{OrderID: "12345"},
			want: true,
		},
		{
			name: "Error present",
			give: application.OrderResult{OrderID: "12345", Error: errors.New("API error")},
			want: false,
		},
		{
			name: "Empty OrderID",
			give: application.OrderResult{},
			want: false,
		},
		{
			name: "Empty OrderID and Error",
			give: application.OrderResult{Error: errors.New("API error")},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.give.IsSuccess())
		})
	}
}
