// Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).
//
// WSO2 LLC. licenses this file to you under the Apache License,
// Version 2.0 (the "License"); you may not use this file except
// in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

// Package netguard answers one question: may aep-api call this URL? It builds
// the http.Client every call to a user-supplied URL goes through (a spec URL, an
// org's model connection), so the SSRF guard is written once.
//
// The guard is in the dialer, not in a URL check: it resolves the host ONCE,
// refuses the whole answer if any address is not public unicast, and dials the
// address it checked. There is no second resolution for a rebinding DNS server
// to answer differently. What a redirect may do is the caller's choice
// (Redirects): a spec fetch follows a few https hops, a model client follows
// none, because Go forwards custom credential headers such as `x-api-key` to a
// redirect's host.
//
// PLATFORM-TOUCHING — reviewed by platform-design-expert; do NOT weaken the
// guard without a new review.
package netguard

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"
)

// ErrNonPublicAddress is the dialer's refusal: the host resolved to at least
// one address that is not public unicast. The resolved address is deliberately
// NOT in the message — echoing it would make the refusal a blind-SSRF oracle
// leaking internal DNS results.
var ErrNonPublicAddress = errors.New("refusing to fetch from non-public address")

// nonPublicNets are the non-public prefixes net.IP.IsGlobalUnicast admits. The
// agents service refuses the same set (services/agents/src/shared/guarded-fetch.ts).
//
// The NAT64 prefixes matter in a NAT64/DNS64 cluster: a DNS64 resolver can
// synthesize an AAAA for an attacker domain that NAT64 then routes to an
// embedded IPv4, including the cloud-metadata endpoint and the pod/service CIDR.
var nonPublicNets = []*net.IPNet{
	mustCIDR("0.0.0.0/8"),      // "this network" (RFC 1122)
	mustCIDR("100.64.0.0/10"),  // CGNAT shared space (RFC 6598)
	mustCIDR("240.0.0.0/4"),    // reserved (RFC 1112)
	mustCIDR("64:ff9b::/96"),   // NAT64 well-known prefix (RFC 6052)
	mustCIDR("64:ff9b:1::/48"), // NAT64 local-use prefix (RFC 8215)
}

func mustCIDR(s string) *net.IPNet {
	_, n, err := net.ParseCIDR(s)
	if err != nil {
		panic(err)
	}
	return n
}

// isPublic reports whether ip is a public unicast address: not loopback,
// link-local (which covers the 169.254.169.254 metadata endpoint), private
// (10/8, 172.16/12, 192.168/16, fc00::/7), unspecified, multicast, or in
// nonPublicNets.
func isPublic(ip net.IP) bool {
	if !ip.IsGlobalUnicast() || ip.IsPrivate() {
		return false
	}
	for _, n := range nonPublicNets {
		if n.Contains(ip) {
			return false
		}
	}
	return true
}

// Redirects is a redirect policy, in http.Client.CheckRedirect's shape.
type Redirects func(req *http.Request, via []*http.Request) error

// FollowHTTPS follows at most maxHops redirects, each to an https URL. This
// closes the https→http downgrade-via-redirect vector at the application layer;
// every hop is still dialled through the guard.
func FollowHTTPS(maxHops int) Redirects {
	return func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "https" {
			return fmt.Errorf("redirect to non-https URL %q is not allowed", req.URL)
		}
		if len(via) >= maxHops {
			return fmt.Errorf("too many redirects (max %d)", maxHops)
		}
		return nil
	}
}

// NoRedirects follows none: the client returns the 3xx response itself, with a
// nil error, and the caller decides what a redirect answer means. A client that
// carries a credential must use it — Go strips Authorization on a cross-host
// redirect but forwards `x-api-key`, so a followed redirect would hand the key
// to another host.
func NoRedirects() Redirects {
	return func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
}

// NewClient returns an http.Client that dials only public addresses, gives up
// after timeout (connect and whole request alike), and follows redirects as
// redirects says. It does not check the URL's scheme or shape: callers validate
// what the user typed first, with a message about the field they typed it in.
func NewClient(timeout time.Duration, redirects Redirects) *http.Client {
	dialer := &net.Dialer{Timeout: timeout}
	return newClient(timeout, redirects, net.DefaultResolver.LookupIP, dialer.DialContext, nil)
}

// lookupFunc and dialFunc are the resolver and the dialer the guard sits
// between; tests replace both to aim a public-looking address at a local
// server, which the real guard would (rightly) refuse to reach.
type (
	lookupFunc func(ctx context.Context, network, host string) ([]net.IP, error)
	dialFunc   func(ctx context.Context, network, addr string) (net.Conn, error)
)

func newClient(timeout time.Duration, redirects Redirects, lookup lookupFunc, dial dialFunc, tlsConfig *tls.Config) *http.Client {
	transport := &http.Transport{
		DialContext:     guardedDial(lookup, dial),
		TLSClientConfig: tlsConfig,
	}
	return &http.Client{Timeout: timeout, Transport: transport, CheckRedirect: redirects}
}

// guardedDial resolves the host ONCE, validates every returned IP, then dials
// the chosen IP directly — eliminating the TOCTOU DNS-rebinding window that
// would exist if the dialer performed its own second lookup. TLS still
// verifies the certificate against the URL's host name, not the IP.
func guardedDial(lookup lookupFunc, dial dialFunc) dialFunc {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, fmt.Errorf("invalid address %q: %w", addr, err)
		}
		ips, err := lookup(ctx, "ip", host)
		if err != nil {
			return nil, err
		}
		if len(ips) == 0 {
			return nil, fmt.Errorf("no IP addresses resolved for %s", host)
		}
		for _, ip := range ips {
			if !isPublic(ip) {
				return nil, ErrNonPublicAddress
			}
		}
		return dial(ctx, network, net.JoinHostPort(ips[0].String(), port))
	}
}
