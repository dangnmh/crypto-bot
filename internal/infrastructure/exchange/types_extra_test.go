//nolint:testpackage // These tests exercise unexported ID conversion variants.
package exchange

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPersonalIDAccessorsCoverNumericVariants(t *testing.T) {
	t.Parallel()

	deal := PersonalOrderDeal{ID: json.Number("123"), OrderID: float64(456)}
	assert.Equal(t, "123", deal.GetID())
	assert.Equal(t, "456", deal.GetOrderID())

	track := PersonalTrackOrderUpdate{ID: int32(789), OrderID: uint64(101112)}
	assert.Equal(t, "789", track.GetID())
	assert.Equal(t, "101112", track.GetOrderID())

	assert.Equal(t, "3.14", interfaceIDToString(float64(3.14)))
	assert.Equal(t, "<nil>", interfaceIDToString(nil))
}
