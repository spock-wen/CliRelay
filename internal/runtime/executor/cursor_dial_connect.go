package executor

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
)

// sendConnectRequest establishes an HTTP CONNECT tunnel through the proxy.
func sendConnectRequest(ctx context.Context, conn net.Conn, target string, proxyURL *url.URL) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodConnect, "http://"+target, nil)
	if err != nil {
		return err
	}
	if proxyURL.User != nil {
		if password, ok := proxyURL.User.Password(); ok {
			req.SetBasicAuth(proxyURL.User.Username(), password)
		} else {
			req.SetBasicAuth(proxyURL.User.Username(), "")
		}
	}
	if errWrite := req.Write(conn); errWrite != nil {
		return fmt.Errorf("cursor proxy connect write: %w", errWrite)
	}
	resp, errRead := http.ReadResponse(bufio.NewReader(conn), req)
	if errRead != nil {
		return fmt.Errorf("cursor proxy connect read: %w", errRead)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("cursor proxy connect: status %d", resp.StatusCode)
	}
	return nil
}
