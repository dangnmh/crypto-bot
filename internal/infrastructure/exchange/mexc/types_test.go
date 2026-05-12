package mexc_test

import (
	"testing"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/exchange/mexc"
)

func TestParseResponse_Success(t *testing.T) {
	t.Parallel()
	body := []byte(`{"success": true, "code": 0, "data": 1234567890}`)
	got, err := mexc.ParseResponse[int64](body, "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != 1234567890 {
		t.Errorf("expected 1234567890, got %d", got)
	}
}

func TestParseResponse_APIError(t *testing.T) {
	t.Parallel()
	body := []byte(`{"success": false, "code": 500, "message": "internal error"}`)
	_, err := mexc.ParseResponse[int64](body, "test_endpoint")
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	apiErr, ok := exchange.IsAPIError(err)
	if !ok {
		t.Fatalf("expected APIError, got %T: %v", err, err)
	}
	if apiErr.Code != 500 {
		t.Errorf("expected code 500, got %d", apiErr.Code)
	}
	if apiErr.Message != "internal error" {
		t.Errorf("expected message 'internal error', got %q", apiErr.Message)
	}
	if apiErr.Path != "test_endpoint" {
		t.Errorf("expected path 'test_endpoint', got %q", apiErr.Path)
	}
}

func TestParseResponse_InvalidJSON(t *testing.T) {
	t.Parallel()
	body := []byte(`not valid json`)
	_, err := mexc.ParseResponse[int64](body, "test")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestParseResponse_StructData(t *testing.T) {
	t.Parallel()
	body := []byte(`{"success": true, "code": 0, "data": {"orderId": "123abc", "ts": 99999}}`)

	type orderResp struct {
		OrderID string `json:"orderId"`
		Ts      int64  `json:"ts"`
	}

	got, err := mexc.ParseResponse[orderResp](body, "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.OrderID != "123abc" {
		t.Errorf("expected orderID '123abc', got %q", got.OrderID)
	}
	if got.Ts != 99999 {
		t.Errorf("expected ts 99999, got %d", got.Ts)
	}
}

func TestParseResponseIgnoreData_Success(t *testing.T) {
	t.Parallel()
	body := []byte(`{"success": true, "code": 0, "data": null}`)
	err := mexc.ParseResponseIgnoreData(body, "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseResponseIgnoreData_Error(t *testing.T) {
	t.Parallel()
	body := []byte(`{"success": false, "code": 403, "message": "forbidden"}`)
	err := mexc.ParseResponseIgnoreData(body, "test")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	apiErr, ok := exchange.IsAPIError(err)
	if !ok {
		t.Fatalf("expected APIError, got %T", err)
	}
	if apiErr.Code != 403 {
		t.Errorf("expected code 403, got %d", apiErr.Code)
	}
}

func TestParseResponse_ArrayData(t *testing.T) {
	t.Parallel()
	body := []byte(`{"success": true, "code": 0, "data": [1, 2, 3]}`)
	got, err := mexc.ParseResponse[[]int](body, "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 3 {
		t.Errorf("expected 3 elements, got %d", len(got))
	}
}
