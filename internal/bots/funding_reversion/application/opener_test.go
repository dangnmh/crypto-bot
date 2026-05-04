package application

import (
	"errors"
	"testing"
)

func TestOrderResultIsSuccess(t *testing.T) {
	tests := []struct {
		name     string
		result   OrderResult
		expected bool
	}{
		{
			name: "Success",
			result: OrderResult{
				OrderID: "12345",
				Error:   nil,
			},
			expected: true,
		},
		{
			name: "Error present",
			result: OrderResult{
				OrderID: "12345",
				Error:   errors.New("API error"),
			},
			expected: false,
		},
		{
			name: "Empty OrderID",
			result: OrderResult{
				OrderID: "",
				Error:   nil,
			},
			expected: false,
		},
		{
			name: "Empty OrderID and Error",
			result: OrderResult{
				OrderID: "",
				Error:   errors.New("API error"),
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := tt.result.IsSuccess()
			if actual != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, actual)
			}
		})
	}
}
