package decmath_test

import (
	"testing"

	"crypto-bot/pkg/decmath"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDecimalConversions(t *testing.T) {
	t.Parallel()

	got, err := decmath.FromString("0.123456789123456789")
	require.NoError(t, err)

	assert.Equal(t, "0.123456789123456789", got.String())
	assert.Equal(t, 0.12345678912345678, decmath.ToFloat(got))
	assert.Equal(t, "1.23", decmath.MustFromString("1.23").String())
}

func TestDecimalTickSnapping(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		price     decmath.Decimal
		tick      decmath.Decimal
		wantFloor string
		wantCeil  string
	}{
		{
			name:      "binary float edge as decimal strings",
			price:     decmath.MustFromString("100.3001"),
			tick:      decmath.MustFromString("0.05"),
			wantFloor: "100.3",
			wantCeil:  "100.35",
		},
		{
			name:      "tiny crypto tick",
			price:     decmath.MustFromString("0.004701"),
			tick:      decmath.MustFromString("0.0001"),
			wantFloor: "0.0047",
			wantCeil:  "0.0048",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.wantFloor, decmath.SnapDecimalToTickFloor(tt.price, tt.tick).String())
			assert.Equal(t, tt.wantCeil, decmath.SnapDecimalToTickCeil(tt.price, tt.tick).String())
		})
	}
}

func TestDecimalPercentConversions(t *testing.T) {
	t.Parallel()

	ratio := decmath.MustFromString("0.005")
	pct := decmath.RatioToPercent(ratio)

	assert.Equal(t, "0.5", pct.String())
	assert.Equal(t, ratio.String(), decmath.PercentToRatio(pct).String())
}

func TestRoundToScale(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		v     float64
		scale int
		want  float64
	}{
		{"round 2dp", 1.234567, 2, 1.23},
		{"round 4dp", 0.00505, 4, 0.0051},
		{"round 0dp", 99.5, 0, 100},
		{"no change", 1.5, 1, 1.5},
		{"negative", -1.235, 2, -1.24},
		{"precision edge", 0.1 + 0.2, 1, 0.3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := decmath.RoundToScale(tt.v, tt.scale)
			assert.InDelta(t, tt.want, got, 1e-10)
		})
	}
}

func TestFloorToScale(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		v     float64
		scale int
		want  float64
	}{
		{"floor 2dp", 1.239, 2, 1.23},
		{"floor exact", 1.23, 2, 1.23},
		{"floor 0dp", 99.9, 0, 99},
		{"floor negative", -1.231, 2, -1.24},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := decmath.FloorToScale(tt.v, tt.scale)
			assert.InDelta(t, tt.want, got, 1e-10)
		})
	}
}

func TestCeilToScale(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		v     float64
		scale int
		want  float64
	}{
		{"ceil 2dp 1", 1.231, 2, 1.24},
		{"ceil exact", 1.23, 2, 1.23},
		{"ceil 0dp", 99.1, 0, 100},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := decmath.CeilToScale(tt.v, tt.scale)
			assert.InDelta(t, tt.want, got, 1e-10)
		})
	}
}

func TestSnapToTickFloor(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		price float64
		tick  float64
		want  float64
	}{
		{"basic", 100.37, 0.05, 100.35},
		{"exact", 100.35, 0.05, 100.35},
		{"small tick", 0.004712, 0.0001, 0.0047},
		{"large tick exact", 100.0, 0.5, 100.0},
		{"large tick", 100.3, 0.5, 100.0},
		{"zero tick", 100.37, 0, 100.37},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := decmath.SnapToTickFloor(tt.price, tt.tick)
			assert.InDelta(t, tt.want, got, 1e-10)
		})
	}
}

func TestSnapToTickCeil(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		price float64
		tick  float64
		want  float64
	}{
		{"basic", 100.31, 0.05, 100.35},
		{"exact", 100.35, 0.05, 100.35},
		{"small tick", 0.004701, 0.0001, 0.0048},
		{"large tick exact", 100.0, 0.5, 100.0},
		{"large tick", 100.1, 0.5, 100.5},
		{"zero tick", 100.31, 0, 100.31},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := decmath.SnapToTickCeil(tt.price, tt.tick)
			assert.InDelta(t, tt.want, got, 1e-10)
		})
	}
}

func TestMul(t *testing.T) {
	t.Parallel()
	got := decmath.Mul(0.1, 0.2)
	assert.Equal(t, 0.02, got)
}

func TestDiv(t *testing.T) {
	t.Parallel()
	assert.InDelta(t, 0.333333333333, decmath.Div(1.0, 3.0), 1e-10)
	assert.Equal(t, 0.0, decmath.Div(1.0, 0.0))
}

func TestAddSub(t *testing.T) {
	t.Parallel()
	assert.Equal(t, 0.3, decmath.Add(0.1, 0.2))
	assert.Equal(t, 0.2, decmath.Sub(0.3, 0.1))
}

func TestEqual(t *testing.T) {
	t.Parallel()
	assert.True(t, decmath.Equal(0.1+0.2, 0.3))
	assert.False(t, decmath.Equal(0.1, 0.2))
}

func TestGreaterThan_LessThan(t *testing.T) {
	t.Parallel()
	assert.True(t, decmath.GreaterThan(0.2, 0.1))
	assert.True(t, decmath.LessThan(0.1, 0.2))
}

func TestFormatPrice(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		price float64
		scale int
		want  string
	}{
		{"4dp", 0.005, 4, "0.0050"},
		{"2dp", 100.0, 2, "100.00"},
		{"3dp", 1.23456, 3, "1.235"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := decmath.FormatPrice(tt.price, tt.scale)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestFormatFloat(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		val  float64
		want string
	}{
		{"integer", 100, "100"},
		{"decimal price", 100.35, "100.35"},
		{"small decimal", 0.00012345, "0.00012345"},
		{"very small decimal", 0.00000001, "0.00000001"},
		{"negative decimal", -123.456, "-123.456"},
		{"no scientific format for tiny", 1e-8, "0.00000001"},
		{"no scientific format for large", 100000000.123, "100000000.123"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := decmath.FormatFloat(tt.val)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestTradingProfitHelpers(t *testing.T) {
	t.Parallel()

	assert.Equal(t, -0.12, decmath.SignedTradingFee(0.12))
	assert.Equal(t, -0.12, decmath.SignedTradingFee(-0.12))
	assert.Equal(t, 0.0, decmath.SignedTradingFee(0))
	assert.Equal(t, 9.88, decmath.ClosedNetProfit(10, 0.12))
	assert.Equal(t, 9.88, decmath.ClosedNetProfit(10, -0.12))
}

func TestCalcSpreadPct(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 0.0, decmath.CalcSpreadPct(0, 101))
	assert.Equal(t, 0.0, decmath.CalcSpreadPct(-1, 101))
	assert.Equal(t, 1.0, decmath.CalcSpreadPct(100, 101))
	assert.Equal(t, 0.5, decmath.CalcSpreadPct(200, 201))
}

func TestAbsInt64(t *testing.T) {
	t.Parallel()
	assert.Equal(t, int64(10), decmath.AbsInt64(10))
	assert.Equal(t, int64(10), decmath.AbsInt64(-10))
	assert.Equal(t, int64(0), decmath.AbsInt64(0))
}

func TestParseFloat(t *testing.T) {
	t.Parallel()
	assert.Equal(t, 12.34, decmath.ParseFloat("12.34"))
	assert.Equal(t, 0.0, decmath.ParseFloat(""))
	assert.Equal(t, 0.0, decmath.ParseFloat("invalid"))
}

func TestDecimalPlaces(t *testing.T) {
	t.Parallel()
	assert.Equal(t, 0, decmath.DecimalPlaces("1"))
	assert.Equal(t, 2, decmath.DecimalPlaces("1.23"))
	assert.Equal(t, 4, decmath.DecimalPlaces("0.0100"))
	assert.Equal(t, 0, decmath.DecimalPlaces(""))
}

func TestParseInt(t *testing.T) {
	t.Parallel()
	assert.Equal(t, 42, decmath.ParseInt("42"))
	assert.Equal(t, 0, decmath.ParseInt(""))
	assert.Equal(t, 0, decmath.ParseInt("invalid"))
}

func TestParseInt64(t *testing.T) {
	t.Parallel()
	assert.Equal(t, int64(42), decmath.ParseInt64("42"))
	assert.Equal(t, int64(42), decmath.ParseInt64(" 42 "))
	assert.Equal(t, int64(0), decmath.ParseInt64(""))
	assert.Equal(t, int64(0), decmath.ParseInt64("invalid"))
	assert.Equal(t, int64(0), decmath.ParseInt64("42x"))
}

func TestFromString_Error(t *testing.T) {
	t.Parallel()

	_, err := decmath.FromString("invalid")
	assert.Error(t, err)

	assert.Panics(t, func() {
		decmath.MustFromString("invalid")
	})
}

func TestTakeDecimalPlaces(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		v             float64
		decimalPlaces int
		want          float64
	}{
		{"round to 3 decimal places (ratio for 1dp percent)", 0.00125, 3, 0.001},
		{"round to 3 decimal places (ratio for 1dp percent) half-up/even", 0.00175, 3, 0.002},
		{"round to 1 decimal place", 12.3456, 1, 12.3},
		{"round to 1 decimal place half-up/even", 12.35, 1, 12.4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := decmath.TakeDecimalPlaces(tt.v, tt.decimalPlaces)
			assert.InDelta(t, tt.want, got, 1e-10)
		})
	}
}
