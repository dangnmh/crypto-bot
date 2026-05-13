package domain_test

import (
	"testing"

	domain "crypto-bot/internal/bots/funding/domain"

	shared "crypto-bot/internal/domain"
)

func TestCalculateATR(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		klines []shared.Kline
		period int
		want   float64
		eps    float64
	}{
		{
			name:   "empty klines",
			klines: nil,
			period: 14,
			want:   0,
		},
		{
			name:   "single kline",
			klines: []shared.Kline{{High: 10, Low: 5, Close: 7}},
			period: 14,
			want:   0,
		},
		{
			name:   "zero period",
			klines: []shared.Kline{{High: 10, Low: 5, Close: 7}, {High: 12, Low: 6, Close: 8}},
			period: 0,
			want:   0,
		},
		{
			name: "two klines, period 1",
			klines: []shared.Kline{
				{High: 10, Low: 5, Close: 8},
				{High: 12, Low: 7, Close: 11},
			},
			period: 1,
			// TR = max(12-7, |12-8|, |7-8|) = max(5, 4, 1) = 5
			want: 5.0,
		},
		{
			name: "three klines, period 2 with Wilder smoothing",
			klines: []shared.Kline{
				{High: 10, Low: 5, Close: 8},
				{High: 12, Low: 7, Close: 11},
				{High: 15, Low: 9, Close: 13},
			},
			period: 2,
			// TR[0] = max(12-7, |12-8|, |7-8|) = 5
			// TR[1] = max(15-9, |15-11|, |9-11|) = 6
			// SMA = (5+6)/2 = 5.5 (no more data for Wilder smoothing)
			want: 5.5,
		},
		{
			name: "period larger than available data",
			klines: []shared.Kline{
				{High: 10, Low: 5, Close: 8},
				{High: 12, Low: 7, Close: 11},
			},
			period: 100,
			// Clamped to n-1 = 1, so SMA of TR[0] = 5
			want: 5.0,
		},
		{
			name: "Wilder smoothing with extra data points",
			klines: []shared.Kline{
				{High: 10, Low: 5, Close: 8},
				{High: 12, Low: 7, Close: 11}, // TR = 5
				{High: 13, Low: 8, Close: 10}, // TR = 5
				{High: 14, Low: 6, Close: 12}, // TR = 8
			},
			period: 2,
			// SMA of first 2 TRs: (5+5)/2 = 5
			// Wilder: (5*(2-1) + 8) / 2 = 13/2 = 6.5
			want: 6.5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			eps := tt.eps
			if eps == 0 {
				eps = 0.01
			}
			got := domain.CalculateATR(tt.klines, tt.period)
			if !almostEqual(got, tt.want, eps) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestATRPercent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		klines []shared.Kline
		period int
		want   float64
	}{
		{
			name:   "empty klines",
			klines: nil,
			period: 14,
			want:   0,
		},
		{
			name: "zero close returns zero",
			klines: []shared.Kline{
				{High: 10, Low: 5, Close: 0},
				{High: 12, Low: 7, Close: 0},
			},
			period: 1,
			want:   0,
		},
		{
			name: "normal calculation",
			klines: []shared.Kline{
				{High: 100, Low: 90, Close: 95},
				{High: 105, Low: 92, Close: 100},
			},
			period: 1,
			// TR = max(105-92, |105-95|, |92-95|) = max(13, 10, 3) = 13
			// ATR = 13, lastClose = 100
			// domain.ATRPercent = (13/100) * 100 = 13.0
			want: 13.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := domain.ATRPercent(tt.klines, tt.period)
			if !almostEqual(got, tt.want, 0.01) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}
