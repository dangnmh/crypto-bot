package events

import "testing"

func TestBaseEventShouldNotify(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		evt  BaseEvent
		want bool
	}{
		{name: "enabled", evt: BaseEvent{SendNotify: true}, want: true},
		{name: "disabled", evt: BaseEvent{SendNotify: false}, want: false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.evt.ShouldNotify(); got != tt.want {
				t.Fatalf("ShouldNotify() = %v, want %v", got, tt.want)
			}
		})
	}
}
