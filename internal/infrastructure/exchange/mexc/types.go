package mexc

// APIResponse is the generic MEXC Futures REST response envelope.
type APIResponse[T any] struct {
	Success bool   `json:"success"`
	Code    int    `json:"code"`
	Data    T      `json:"data"`
	Message string `json:"message,omitempty"`
}
