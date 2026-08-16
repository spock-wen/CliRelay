package executor

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/url"
	"time"
)

// proxyDialTLSContext returns an http2 DialTLSContext that tunnels through an
// HTTP(S) proxy via CONNECT, then upgrades to TLS with the target SNI.
// Unsupported proxy schemes (e.g. socks5) fall back to direct dialing.
func proxyDialTLSContext(proxyURL *url.URL) func(ctx context.Context, network, addr string, cfg *tls.Config) (net.Conn, error) {
	switch proxyURL.Scheme {
	case "http", "https":
	default:
		return nil
	}
	dialer := &net.Dialer{Timeout: 30 * time.Second}
	return func(ctx context.Context, network, addr string, cfg *tls.Config) (net.Conn, error) {
		rawConn, err := dialer.DialContext(ctx, network, proxyURL.Host)
		if err != nil {
			return nil, fmt.Errorf("cursor proxy dial %s: %w", proxyURL.Host, err)
		}
		if errConnect := sendConnectRequest(ctx, rawConn, addr, proxyURL); errConnect != nil {
			_ = rawConn.Close()
			return nil, errConnect
		}
		if cfg == nil {
			cfg = &tls.Config{}
		}
		if cfg.ServerName == "" {
			host, _, errSplit := net.SplitHostPort(addr)
			if errSplit == nil {
				cfg = cfg.Clone()
				cfg.ServerName = host
			}
		}
		tlsConn := tls.Client(rawConn, cfg)
		if errHandshake := tlsConn.HandshakeContext(ctx); errHandshake != nil {
			_ = tlsConn.Close()
			return nil, fmt.Errorf("cursor proxy tls handshake: %w", errHandshake)
		}
		return tlsConn, nil
	}
}
