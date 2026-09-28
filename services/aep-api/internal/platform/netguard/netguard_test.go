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

package netguard

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Public-looking addresses from TEST-NET-3 (RFC 5737), which the guard treats as
// public; the tests' dial seam aims them at a local server.
var (
	publicA = net.ParseIP("203.0.113.10")
	publicB = net.ParseIP("203.0.113.20")
)

// The whole refused set the design names, plus the edges around it.
func TestIsPublic(t *testing.T) {
	cases := []struct {
		name string
		ip   string
		want bool
	}{
		{"loopback v4", "127.0.0.1", false},
		{"loopback v4 range", "127.1.2.3", false},
		{"loopback v6", "::1", false},
		{"cloud metadata", "169.254.169.254", false},
		{"link-local v4", "169.254.0.1", false},
		{"10/8", "10.0.0.1", false},
		{"10/8 top", "10.255.255.255", false},
		{"172.16/12 bottom", "172.16.0.1", false},
		{"172.16/12 top", "172.31.255.255", false},
		{"192.168/16", "192.168.1.1", false},
		{"CGNAT bottom", "100.64.0.1", false},
		{"CGNAT top", "100.127.255.255", false},
		{"NAT64 metadata", "64:ff9b::a9fe:a9fe", false},
		{"NAT64 private", "64:ff9b::a00:1", false},
		{"IPv6 ULA fc00", "fc00::1", false},
		{"IPv6 ULA fd", "fd12:3456::1", false},
		{"IPv6 link-local", "fe80::1", false},
		{"unspecified v4", "0.0.0.0", false},
		{"this network 0/8", "0.1.2.3", false},
		{"reserved 240/4", "240.0.0.1", false},
		{"reserved 240/4 top", "255.255.255.254", false},
		{"broadcast", "255.255.255.255", false},
		{"NAT64 local-use", "64:ff9b:1::a00:1", false},
		{"just above 0/8", "1.0.0.1", true},
		{"just below 240/4", "239.255.255.255", false},
		{"just outside NAT64 local-use", "64:ff9b:2::1", true},
		{"unspecified v6", "::", false},
		{"multicast v4", "224.0.0.1", false},
		{"multicast v6", "ff02::1", false},
		{"v4-mapped loopback", "::ffff:127.0.0.1", false},
		{"v4-mapped private", "::ffff:10.0.0.1", false},
		{"public v4", "8.8.8.8", true},
		{"public v6", "2606:4700:4700::1111", true},
		{"just below CGNAT", "100.63.255.255", true},
		{"just above CGNAT", "100.128.0.0", true},
		{"just above 172.16/12", "172.32.0.1", true},
		{"TEST-NET-3 stand-in", publicA.String(), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ip := net.ParseIP(tc.ip)
			if ip == nil {
				t.Fatalf("bad fixture %q", tc.ip)
			}
			if got := isPublic(ip); got != tc.want {
				t.Fatalf("isPublic(%s) = %v, want %v", tc.ip, got, tc.want)
			}
		})
	}
}

// recordingDial never touches the network: it records what it was asked to
// dial and fails, so a test can see whether the guard let a dial through.
type recordingDial struct {
	mu    sync.Mutex
	addrs []string
}

var errDialed = errors.New("dialed")

func (r *recordingDial) dial(_ context.Context, _, addr string) (net.Conn, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.addrs = append(r.addrs, addr)
	return nil, errDialed
}

func resolveTo(ips ...string) lookupFunc {
	return func(context.Context, string, string) ([]net.IP, error) {
		out := make([]net.IP, len(ips))
		for i, s := range ips {
			out[i] = net.ParseIP(s)
		}
		return out, nil
	}
}

// Every refused address is refused at the dialer, before any connect, and the
// refusal never echoes the address.
func TestGuardedDial_RefusesNonPublicAnswers(t *testing.T) {
	for _, answer := range [][]string{
		{"127.0.0.1"},
		{"::1"},
		{"169.254.169.254"},
		{"10.1.2.3"},
		{"172.20.0.5"},
		{"192.168.0.10"},
		{"100.100.100.100"},
		{"64:ff9b::a9fe:a9fe"},
		{"fd00::5"},
		{"fe80::1"},
		// All or nothing: one private address in the answer refuses the host,
		// whatever order it comes in.
		{"8.8.8.8", "10.0.0.1"},
		{"10.0.0.1", "8.8.8.8"},
	} {
		t.Run(strings.Join(answer, ","), func(t *testing.T) {
			rec := &recordingDial{}
			_, err := guardedDial(resolveTo(answer...), rec.dial)(context.Background(), "tcp", "evil.example.com:443")
			if !errors.Is(err, ErrNonPublicAddress) {
				t.Fatalf("err = %v, want ErrNonPublicAddress", err)
			}
			for _, ip := range answer {
				if strings.Contains(err.Error(), ip) {
					t.Fatalf("refusal %q echoes the resolved address %s", err, ip)
				}
			}
			if len(rec.addrs) != 0 {
				t.Fatalf("a refused host was dialled: %v", rec.addrs)
			}
		})
	}
}

// The dial goes to the address that was checked, never back through a
// resolver that could answer differently the second time.
func TestGuardedDial_DialsTheCheckedAddress(t *testing.T) {
	rec := &recordingDial{}
	_, err := guardedDial(resolveTo("8.8.8.8", "8.8.4.4"), rec.dial)(context.Background(), "tcp", "api.example.com:443")
	if !errors.Is(err, errDialed) {
		t.Fatalf("err = %v, want the dial to be attempted", err)
	}
	if len(rec.addrs) != 1 || rec.addrs[0] != "8.8.8.8:443" {
		t.Fatalf("dialled %v, want exactly [8.8.8.8:443]", rec.addrs)
	}
}

func TestGuardedDial_EmptyAnswerIsAnError(t *testing.T) {
	rec := &recordingDial{}
	if _, err := guardedDial(resolveTo(), rec.dial)(context.Background(), "tcp", "nothing.example.com:443"); err == nil {
		t.Fatal("an empty DNS answer must not dial")
	}
	if len(rec.addrs) != 0 {
		t.Fatalf("dialled %v on an empty answer", rec.addrs)
	}
}

// A rebinding resolver answers public, then private. Each connection resolves
// once and checks what it got, so the second answer is refused rather than
// trusted on the strength of the first.
func TestGuardedDial_ResolverThatRebindsToPrivate(t *testing.T) {
	var calls atomic.Int32
	rebinding := func(context.Context, string, string) ([]net.IP, error) {
		if calls.Add(1) == 1 {
			return []net.IP{net.ParseIP("8.8.8.8")}, nil
		}
		return []net.IP{net.ParseIP("169.254.169.254")}, nil
	}
	rec := &recordingDial{}
	dial := guardedDial(rebinding, rec.dial)

	if _, err := dial(context.Background(), "tcp", "rebind.example.com:443"); !errors.Is(err, errDialed) {
		t.Fatalf("first (public) answer: err = %v, want the dial attempted", err)
	}
	if _, err := dial(context.Background(), "tcp", "rebind.example.com:443"); !errors.Is(err, ErrNonPublicAddress) {
		t.Fatalf("second (private) answer: err = %v, want ErrNonPublicAddress", err)
	}
	if len(rec.addrs) != 1 || rec.addrs[0] != "8.8.8.8:443" {
		t.Fatalf("dialled %v, want only the checked public address", rec.addrs)
	}
}

// The same rebinding resolver through the real client: the first request
// reaches the server, the second never connects.
func TestClient_ResolverThatRebindsToPrivate(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
	}))
	defer srv.Close()

	var calls atomic.Int32
	rebinding := func(context.Context, string, string) ([]net.IP, error) {
		if calls.Add(1) == 1 {
			return []net.IP{publicA}, nil
		}
		return []net.IP{net.ParseIP("10.0.0.7")}, nil
	}
	hosts := map[string]string{publicA.String(): srv.Listener.Addr().String()}
	c := testClient(t, srv, NoRedirects(), rebinding, hosts)
	c.Transport.(*http.Transport).DisableKeepAlives = true // every request dials afresh

	if resp, err := c.Get("https://rebind.example.com/"); err != nil {
		t.Fatalf("first request: %v", err)
	} else {
		resp.Body.Close()
	}
	if _, err := c.Get("https://rebind.example.com/"); !errors.Is(err, ErrNonPublicAddress) {
		t.Fatalf("second request: err = %v, want ErrNonPublicAddress", err)
	}
	if hits.Load() != 1 {
		t.Fatalf("server saw %d requests, want 1", hits.Load())
	}
}

// The model client's reason to exist: a redirect to another public host is
// NOT followed, so the org's key never reaches it. Go forwards x-api-key on a
// cross-host redirect (it strips only Authorization and cookies), so following
// would leak it.
func TestClient_NoRedirects_KeyNeverReachesTheRedirectTarget(t *testing.T) {
	const key = "sk-test-not-a-real-key-0000"
	var bHits atomic.Int32
	b := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bHits.Add(1)
		if r.Header.Get("x-api-key") != "" || r.Header.Get("Authorization") != "" {
			t.Errorf("the redirect target received a credential header")
		}
	}))
	defer b.Close()
	a := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != key {
			t.Errorf("the first host did not receive the key it was sent")
		}
		http.Redirect(w, r, "https://b.example.com/v1/models", http.StatusFound)
	}))
	defer a.Close()

	lookup := func(_ context.Context, _, host string) ([]net.IP, error) {
		switch host {
		case "a.example.com":
			return []net.IP{publicA}, nil
		case "b.example.com":
			return []net.IP{publicB}, nil
		}
		return nil, errors.New("unknown host " + host)
	}
	hosts := map[string]string{
		publicA.String(): a.Listener.Addr().String(),
		publicB.String(): b.Listener.Addr().String(),
	}

	send := func(c *http.Client) *http.Response {
		t.Helper()
		req, _ := http.NewRequest(http.MethodGet, "https://a.example.com/v1/models", nil)
		req.Header.Set("x-api-key", key)
		resp, err := c.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		resp.Body.Close()
		return resp
	}

	resp := send(testClient(t, a, NoRedirects(), lookup, hosts, b))
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want the 302 handed back unfollowed", resp.StatusCode)
	}
	if got := resp.Header.Get("Location"); got != "https://b.example.com/v1/models" {
		t.Fatalf("Location = %q", got)
	}
	if bHits.Load() != 0 {
		t.Fatalf("the redirect target was contacted %d time(s)", bHits.Load())
	}
}

// The spec fetch's policy, for contrast: https hops are followed up to the cap;
// a downgrade to http and one hop too many are refused.
func TestClient_FollowHTTPS(t *testing.T) {
	var hops atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/done":
			_, _ = w.Write([]byte("ok"))
		case "/downgrade":
			http.Redirect(w, r, "http://a.example.com/done", http.StatusFound)
		default: // /hop: count down n more redirects
			if hops.Add(-1) >= 0 {
				http.Redirect(w, r, "/hop", http.StatusFound)
				return
			}
			http.Redirect(w, r, "/done", http.StatusFound)
		}
	}))
	defer srv.Close()
	hosts := map[string]string{publicA.String(): srv.Listener.Addr().String()}
	c := testClient(t, srv, FollowHTTPS(5), resolveTo(publicA.String()), hosts)

	hops.Store(3) // 3 hops + the final redirect to /done = 5 redirects
	resp, err := c.Get("https://a.example.com/hop")
	if err != nil {
		t.Fatalf("5 https redirects must be followed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 after the hops", resp.StatusCode)
	}

	hops.Store(4) // 6 redirects
	if _, err := c.Get("https://a.example.com/hop"); err == nil || !strings.Contains(err.Error(), "too many redirects (max 5)") {
		t.Fatalf("a sixth redirect: err = %v, want too many redirects", err)
	}

	if _, err := c.Get("https://a.example.com/downgrade"); err == nil || !strings.Contains(err.Error(), "non-https") {
		t.Fatalf("an http redirect: err = %v, want it refused", err)
	}
}

// NewClient's real resolver refuses a literal loopback URL without any seam.
func TestNewClient_RefusesLoopback(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("the loopback server was reached")
	}))
	defer srv.Close()
	_, err := NewClient(5*time.Second, NoRedirects()).Get(srv.URL)
	if !errors.Is(err, ErrNonPublicAddress) {
		t.Fatalf("err = %v, want ErrNonPublicAddress", err)
	}
}

// testClient builds the guarded client with the resolver under test, a dialer
// that maps each checked public address to a local listener (the guard has
// already refused anything else), and TLS trust for the test servers' shared
// certificate, which covers *.example.com.
func testClient(t *testing.T, srv *httptest.Server, redirects Redirects, lookup lookupFunc, hosts map[string]string, more ...*httptest.Server) *http.Client {
	t.Helper()
	pool := x509.NewCertPool()
	for _, s := range append([]*httptest.Server{srv}, more...) {
		pool.AddCert(s.Certificate())
	}
	var d net.Dialer
	dial := func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, _, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		local, ok := hosts[host]
		if !ok {
			t.Errorf("dialled %s, which the test did not map", addr)
			return nil, errors.New("unmapped address")
		}
		return d.DialContext(ctx, network, local)
	}
	return newClient(5*time.Second, redirects, lookup, dial, &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12})
}
