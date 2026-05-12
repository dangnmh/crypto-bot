package application_test

import (
	"crypto-bot/internal/bots/funding_reversion/application"
	"errors"
	"testing"
)

func TestOrderResultIsSuccess(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		result   application.OrderResult
		expected bool
	}{
		{
			name: "Success",
			result: application.OrderResult{
				OrderID: "12345",
				Error:   nil,
			},
			expected: true,
		},
		{
			name: "Error present",
			result: application.OrderResult{
				OrderID: "12345",
				Error:   errors.New("API error"),
			},
			expected: false,
		},
		{
			name: "Empty OrderID",
			result: application.OrderResult{
				OrderID: "",
				Error:   nil,
			},
			expected: false,
		},
		{
			name: "Empty OrderID and Error",
			result: application.OrderResult{
				OrderID: "",
				Error:   errors.New("API error"),
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			actual := tt.result.IsSuccess()
			if actual != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, actual)
			}
		})
	}
}
