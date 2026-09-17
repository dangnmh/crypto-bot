package network

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"crypto-bot/pkg/httpclient"

	"github.com/gorilla/websocket"
)

// ResolveLocalTCPAddr parses and validates an IP string into a *net.TCPAddr with an ephemeral port (0).
// Returns nil if ip is empty.
func ResolveLocalTCPAddr(ip string) (*net.TCPAddr, error) {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return nil, nil
	}
	addr, err := net.ResolveTCPAddr("tcp", net.JoinHostPort(ip, "0"))
	if err != nil {
		return nil, fmt.Errorf("resolve local TCP addr %q: %w", ip, err)
	}
	return addr, nil
}

// ParseProxyURL parses a proxy URL string (http, https, socks5).
// Returns nil if rawURL is empty.
func ParseProxyURL(rawURL string) (*url.URL, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil, nil
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parse proxy URL %q: %w", rawURL, err)
	}
	return u, nil
}

// NewAccountHTTPClient creates an HTTP client pool dedicated to an account with optional outbound IP and proxy.
func NewAccountHTTPClient(outboundIP, proxyURL string, cfg httpclient.PoolConfig) (*http.Client, error) {
	localAddr, err := ResolveLocalTCPAddr(outboundIP)
	if err != nil {
		return nil, err
	}
	cfg.LocalAddr = localAddr

	u, err := ParseProxyURL(proxyURL)
	if err != nil {
		return nil, err
	}
	cfg.ProxyURL = u

	return httpclient.NewPool(cfg), nil
}

// NewAccountWSDialer creates a gorilla/websocket.Dialer dedicated to an account with optional outbound IP and proxy.
func NewAccountWSDialer(outboundIP, proxyURL string, handshakeTimeout time.Duration) (*websocket.Dialer, error) {
	if handshakeTimeout <= 0 {
		handshakeTimeout = 15 * time.Second
	}
	localAddr, err := ResolveLocalTCPAddr(outboundIP)
	if err != nil {
		return nil, err
	}

	dialer := &websocket.Dialer{
		HandshakeTimeout: handshakeTimeout,
		Subprotocols:     []string{},
	}

	if localAddr != nil {
		dialer.NetDialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			netDialer := &net.Dialer{
				LocalAddr: localAddr,
				Timeout:   handshakeTimeout,
			}
			return netDialer.DialContext(ctx, network, addr)
		}
	}

	if proxyURL != "" {
		u, err := ParseProxyURL(proxyURL)
		if err != nil {
			return nil, err
		}
		dialer.Proxy = http.ProxyURL(u)
	}

	return dialer, nil
}
