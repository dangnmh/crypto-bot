package formatutil_test

import (
	"testing"

	"crypto-bot/pkg/formatutil"
)

func TestFormatPrice(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		price float64
		want  string
	}{
		{"zero", 0, "0.000000"},
		{"very small positive", 0.05, "0.050000"},
		{"very small negative", -0.05, "-0.050000"},
		{"small positive", 0.5, "0.500000"},
		{"small negative", -0.5, "-0.500000"},
		{"normal positive", 123.456, "123.456000"},
		{"normal negative", -123.456, "-123.456000"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := formatutil.FormatPrice(tt.price)
			if got != tt.want {
				t.Errorf("FormatPrice() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFormatPriceWithCommas(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		price float64
		want  string
	}{
		{"zero", 0, "0.000000"},
		{"small", 0.5, "0.500000"},
		{"thousand price", 1234.56, "1,234.560000"},
		{"million price", 1234567.89, "1,234,567.890000"},
		{"negative thousand price", -1234.56, "-1,234.560000"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := formatutil.FormatPriceWithCommas(tt.price)
			if got != tt.want {
				t.Errorf("FormatPriceWithCommas() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFormatUSDWithCommas(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		val  float64
		want string
	}{
		{"zero", 0, "0.00"},
		{"normal", 123.456, "123.46"},
		{"thousand", 1234.56, "1,234.56"},
		{"million", 1234567.89, "1,234,567.89"},
		{"negative thousand", -1234.56, "-1,234.56"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := formatutil.FormatUSDWithCommas(tt.val)
			if got != tt.want {
				t.Errorf("FormatUSDWithCommas() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFormatNetPnL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		net  float64
		pct  float64
		want string
	}{
		{"positive", 123.45, 1.25, "+$123.4500 (+1.25%)"},
		{"negative", -123.45, -1.25, "-$123.4500 (-1.25%)"},
		{"zero", 0, 0, "$0.0000 (0.00%)"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := formatutil.FormatNetPnL(tt.net, tt.pct)
			if got != tt.want {
				t.Errorf("FormatNetPnL() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFormatFundingFee(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		val  float64
		want string
	}{
		{"zero", 0, "$0.0000"},
		{"positive", 12.34, "+$12.3400"},
		{"negative", -12.34, "-$12.3400"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := formatutil.FormatFundingFee(tt.val)
			if got != tt.want {
				t.Errorf("FormatFundingFee() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFormatDuration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		ms   int64
		want string
	}{
		{"zero", 0, "0s"},
		{"negative", -10, "0s"},
		{"seconds", 45000, "45s"},
		{"minutes and seconds", 75000, "1m 15s"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := formatutil.FormatDuration(tt.ms)
			if got != tt.want {
				t.Errorf("FormatDuration() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFormatFloatMax4(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		val  float64
		want string
	}{
		{"zero", 0, "0"},
		{"integer", 123, "123"},
		{"one decimal", 123.4, "123.4"},
		{"two decimals", 123.45, "123.45"},
		{"three decimals", 123.456, "123.456"},
		{"four decimals", 123.4567, "123.4567"},
		{"five decimals rounding down", 123.45674, "123.4567"},
		{"five decimals rounding up", 123.45678, "123.4568"},
		{"trailing zero decimal", 1.250, "1.25"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := formatutil.FormatFloatMax4(tt.val)
			if got != tt.want {
				t.Errorf("FormatFloatMax4() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFormatCompactUSD(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		val  float64
		want string
	}{
		{"zero", 0, "0"},
		{"negative sub-thousand", -0.5, "-0.5"},
		{"under thousand", 999.9, "999.9"},
		{"exact thousand", 1000, "1k"},
		{"thousand decimal", 1250, "1.25k"},
		{"thousand decimal rounding", 1256, "1.26k"},
		{"exact million", 22000000, "22m"},
		{"million decimal", 22500000, "22.5m"},
		{"exact billion", 1500000000, "1.5b"},
		{"negative exact thousand", -12000, "-12k"},
		{"negative exact million", -22000000, "-22m"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := formatutil.FormatCompactUSD(tt.val)
			if got != tt.want {
				t.Errorf("FormatCompactUSD() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetNormalizedSymbol(t *testing.T) {
	t.Parallel()

	tests := []struct {
		symbol string
		want   string
	}{
		{"BTC-USDT-SWAP", "BTC"},
		{"ETH-USDT-PERPETUAL", "ETH"},
		{"SOL-SWAP-USDT", "SOL"},
		{"ADA_USDT", "ADA"},
		{"XRP-USDT", "XRP"},
		{"DOGEUSDTM", "DOGE"},
		{"DOTUSDT", "DOT"},
		{"LINK_USD", "LINK"},
		{"UNI-USD", "UNI"},
		{"LTCUSD", "LTC"},
		{"H-USDT", "HOME"},
	}

	for _, tt := range tests {
		t.Run(tt.symbol, func(t *testing.T) {
			t.Parallel()
			got := formatutil.GetNormalizedSymbol(tt.symbol)
			if got != tt.want {
				t.Errorf("GetNormalizedSymbol(%q) = %q, want %q", tt.symbol, got, tt.want)
			}
		})
	}
}
