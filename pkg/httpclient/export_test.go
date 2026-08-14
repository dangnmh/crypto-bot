package httpclient

import (
	"net/http"
	"reflect"
	"unsafe"

	"github.com/hashicorp/go-retryablehttp"
)

// UnwrapClientTransport unwraps the traceRoundTripper to return the underlying *http.Transport.
func UnwrapClientTransport(client *http.Client) (*http.Transport, bool) {
	if client == nil {
		return nil, false
	}

	rt := client.Transport
	if retryRT, ok := rt.(*retryablehttp.RoundTripper); ok {
		rt = retryRT.Client.HTTPClient.Transport
	}

	// Unwrap otelhttp.Transport if wrapped
	if otelRT := unwrapOTelTransport(rt); otelRT != nil {
		rt = otelRT
	}

	if traceRT, ok := rt.(*traceRoundTripper); ok {
		tr, ok := traceRT.next.(*http.Transport)
		return tr, ok
	}

	return nil, false
}

func unwrapOTelTransport(rt http.RoundTripper) http.RoundTripper {
	if rt == nil {
		return nil
	}
	val := reflect.ValueOf(rt)
	if val.Kind() == reflect.Pointer {
		val = val.Elem()
	}
	if val.Kind() == reflect.Struct {
		field := val.FieldByName("rt")
		if field.IsValid() {
			//nolint:gosec // Using reflect.NewAt to bypass unexported field restrictions for testing
			field = reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem()
			if next, ok := reflect.TypeAssert[http.RoundTripper](field); ok {
				return next
			}
		}
	}
	return nil
}
