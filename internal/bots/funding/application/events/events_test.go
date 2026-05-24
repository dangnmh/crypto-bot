package events_test

import (
	"testing"

	"crypto-bot/internal/bots/funding/application/events"
)

func TestBaseEventShouldNotify(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		evt  events.BaseEvent
		want bool
	}{
		{name: "enabled", evt: events.BaseEvent{SendNotify: true}, want: true},
		{name: "disabled", evt: events.BaseEvent{SendNotify: false}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.evt.ShouldNotify(); got != tt.want {
				t.Fatalf("ShouldNotify() = %v, want %v", got, tt.want)
			}
		})
	}
}
