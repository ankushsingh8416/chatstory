// Package netutil holds small networking helpers shared across packages that
// need to make outbound connections to org-supplied hosts (webhooks, custom
// actions, and — for org-owned data connections — Qdrant/Postgres).
package netutil

import (
	"context"
	"fmt"
	"net"
	"time"
)

// SSRFSafeDialer returns a dial function that blocks connections to
// private/loopback/link-local IPs after DNS resolution, so a caller-supplied
// hostname can't be used to reach internal services (including via DNS
// rebinding, since resolution happens at dial time, not just at input
// validation time). Use it as an http.Transport.DialContext for HTTP calls,
// or as a pgconn.Config.DialFunc for raw Postgres connections — both share
// this exact `func(ctx, network, addr) (net.Conn, error)` signature.
func SSRFSafeDialer() func(ctx context.Context, network, addr string) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}

		ips, err := net.DefaultResolver.LookupHost(ctx, host)
		if err != nil {
			return nil, err
		}

		for _, ipStr := range ips {
			ip := net.ParseIP(ipStr)
			if ip == nil {
				continue
			}
			if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
				ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
				return nil, fmt.Errorf("connection to private address %s is not allowed", ipStr)
			}
		}

		// Connect to first resolved IP
		return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0], port))
	}
}
