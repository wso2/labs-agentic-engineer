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

package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"math/big"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/wso2/aep/aep-api/internal/platform/auth/jwtassertion"
	"github.com/wso2/aep/aep-api/internal/platform/tenant"
)

// openCycle is an open cycle owned by org.
func openCycle(org string) RunnerCycle { return RunnerCycle{OrgHandle: org, Open: true} }

// statusOf extracts the HTTP status an *HTTPError carries (0 if it is not
// one).
func statusOf(err error) int {
	var he *HTTPError
	if errors.As(err, &he) {
		return he.Status
	}
	return 0
}

// A JWT signed by a key the IdP does not publish is not a runner-callback
// credential: it must 401 the same as garbage. BFF-signed Task JWTs are no
// longer minted, so any still in flight lands here.
func TestRunnerAuthorizer_ForeignKeyJWTRejected(t *testing.T) {
	verifier, _ := newPublisherVerifier(t)
	a := NewRunnerAuthorizer(verifier, func(context.Context, string) (RunnerCycle, error) {
		t.Fatal("cycle lookup must not run for a foreign-key JWT")
		return RunnerCycle{}, nil
	})

	foreign, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tok, err := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.RegisteredClaims{
		Issuer:    "aep-bff",
		Audience:  jwt.ClaimStrings{"git-service"},
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
	}).SignedString(foreign)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	_, err = a.Authorize(context.Background(), "Bearer "+tok, "task-1")
	if got := statusOf(err); got != 401 {
		t.Fatalf("status = %d, want 401 (err=%v)", got, err)
	}
}

func TestRunnerAuthorizer_BadBearer(t *testing.T) {
	a := NewRunnerAuthorizer(nil, func(context.Context, string) (RunnerCycle, error) {
		return RunnerCycle{}, nil
	})

	cases := map[string]string{
		"absent":       "",
		"wrong scheme": "Token abc.def.ghi",
		"too short":    "Bearer",
	}
	for name, header := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := a.Authorize(context.Background(), header, "task-1")
			if got := statusOf(err); got != 401 {
				t.Fatalf("status = %d, want 401 (err=%v)", got, err)
			}
		})
	}
}

func TestRunnerAuthorizer_GarbageJWT(t *testing.T) {
	verifier, _ := newPublisherVerifier(t)
	a := NewRunnerAuthorizer(verifier, func(context.Context, string) (RunnerCycle, error) {
		t.Fatal("cycle lookup must not run for garbage jwt")
		return RunnerCycle{}, nil
	})
	_, err := a.Authorize(context.Background(), "Bearer not-a-real-token", "task-1")
	if got := statusOf(err); got != 401 {
		t.Fatalf("status = %d, want 401 (err=%v)", got, err)
	}
}

// newPublisherVerifier stands up an httptest JWKS server backed by a fresh RSA
// key and returns a verifier plus a mint func that signs publisher-cc tokens
// (aud = "aep-publisher-<org>", ouHandle = <ouHandle>) the verifier accepts.
const pubIssuer, pubAudPrefix = "platform-idp", "aep-publisher-"

func newPublisherVerifier(t *testing.T) (*PublisherTokenVerifier, func(org, ouHandle string) string) {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	const kid = "test-kid"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(jwtassertion.JWKS{Keys: []jwtassertion.JSONWebKey{{
			Kty: "RSA", Kid: kid, Use: "sig", Alg: "RS256",
			N: base64.RawURLEncoding.EncodeToString(priv.N.Bytes()),
			E: base64.RawURLEncoding.EncodeToString(big.NewInt(int64(priv.E)).Bytes()),
		}}})
	}))
	t.Cleanup(srv.Close)

	v := NewPublisherTokenVerifier(jwtassertion.NewJWKSCache(srv.URL), pubIssuer, pubAudPrefix)
	if v == nil {
		t.Fatal("NewPublisherTokenVerifier returned nil")
	}
	mint := func(org, ouHandle string) string {
		claims := PublisherClaims{
			RegisteredClaims: jwt.RegisteredClaims{
				Issuer:    pubIssuer,
				Audience:  jwt.ClaimStrings{pubAudPrefix + org},
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			},
			OuHandle: ouHandle,
		}
		tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
		tok.Header["kid"] = kid
		signed, err := tok.SignedString(priv)
		if err != nil {
			t.Fatalf("sign publisher token: %v", err)
		}
		return signed
	}
	return v, mint
}

// Publisher CC is the only runner-callback credential. The token's org MUST
// match the cycle's owning org (runner.go's cross-org fence) — org-A's token
// cannot refresh an org-B cycle it names.
func TestRunnerAuthorizer_PublisherCC(t *testing.T) {
	verifier, mint := newPublisherVerifier(t)

	t.Run("valid + task-org matches → ok, publisher source", func(t *testing.T) {
		a := NewRunnerAuthorizer(verifier,
			func(context.Context, string) (RunnerCycle, error) { return openCycle("org-a"), nil })
		caller, err := a.Authorize(context.Background(), "Bearer "+mint("org-a", "org-a"), "task-1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if caller.Org != tenant.OrgHandle("org-a") {
			t.Errorf("org = %q, want org-a", caller.Org)
		}
		if caller.Source != tenant.SourcePublisherCC {
			t.Errorf("source = %v, want SourcePublisherCC", caller.Source)
		}
	})

	t.Run("token org ≠ task org → 403 (cross-org fence)", func(t *testing.T) {
		a := NewRunnerAuthorizer(verifier,
			func(context.Context, string) (RunnerCycle, error) { return openCycle("org-b"), nil })
		_, err := a.Authorize(context.Background(), "Bearer "+mint("org-a", "org-a"), "task-1")
		if got := statusOf(err); got != 403 {
			t.Fatalf("status = %d, want 403 (err=%v)", got, err)
		}
	})

	t.Run("task lookup fails → 403", func(t *testing.T) {
		a := NewRunnerAuthorizer(verifier,
			func(context.Context, string) (RunnerCycle, error) { return RunnerCycle{}, errors.New("not found") })
		_, err := a.Authorize(context.Background(), "Bearer "+mint("org-a", "org-a"), "task-1")
		if got := statusOf(err); got != 403 {
			t.Fatalf("status = %d, want 403 (err=%v)", got, err)
		}
	})

	// A cycle that belongs to ANOTHER org and a cycle that does not exist must be
	// indistinguishable to the caller. A different message on the cross-org arm
	// turns a valid org-A token into an oracle for whether any given cycle id
	// exists on the platform — the id is all the prober has to supply, and a
	// distinguishable answer is the whole exploit. Same status AND same message;
	// only the log may tell them apart.
	t.Run("another org's cycle is indistinguishable from a missing one", func(t *testing.T) {
		answer := func(lookup CycleLookup) (int, string) {
			a := NewRunnerAuthorizer(verifier, lookup)
			_, err := a.Authorize(context.Background(), "Bearer "+mint("org-a", "org-a"), "cycle-1")
			var he *HTTPError
			if !errors.As(err, &he) {
				t.Fatalf("want an *HTTPError, got %v", err)
			}
			return he.Status, he.Message
		}
		missingStatus, missingMsg := answer(
			func(context.Context, string) (RunnerCycle, error) { return RunnerCycle{}, errors.New("no such cycle") })
		otherStatus, otherMsg := answer(
			func(context.Context, string) (RunnerCycle, error) { return openCycle("org-b"), nil })

		if missingStatus != otherStatus || missingMsg != otherMsg {
			t.Fatalf("the two answers leak which cycles exist: missing=%d/%q, other-org=%d/%q",
				missingStatus, missingMsg, otherStatus, otherMsg)
		}
	})
}

// Org is taken from the verified publisher token — never from anything the
// caller could spoof in the request.
func TestRunnerAuthorizer_OrgFromClaimNotInput(t *testing.T) {
	verifier, mint := newPublisherVerifier(t)
	a := NewRunnerAuthorizer(verifier, func(context.Context, string) (RunnerCycle, error) { return openCycle("org-zed"), nil })
	caller, err := a.Authorize(context.Background(), "Bearer "+mint("org-zed", "org-zed"), "task-9")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if caller.Org != tenant.OrgHandle("org-zed") {
		t.Errorf("org = %q, want org-zed (from the signed claim)", caller.Org)
	}
	if caller.Source != tenant.SourcePublisherCC {
		t.Errorf("source = %v, want SourcePublisherCC", caller.Source)
	}
}

// A runner of a CLOSED cycle is refused: its run has moved on, and the agent
// calling may be a zombie whose pod started after the cycle closed (a
// startup_failed cycle whose Job had not been suspended yet). The answer is
// the same 403 an unknown or foreign cycle gets, so a closed cycle is no
// oracle either; the org check comes first, so another org's closed cycle
// logs as an org mismatch, never as "closed".
func TestRunnerAuthorizer_ClosedCycleIsRefused(t *testing.T) {
	verifier, mint := newPublisherVerifier(t)
	answer := func(lookup CycleLookup) (int, string, error) {
		a := NewRunnerAuthorizer(verifier, lookup)
		_, err := a.Authorize(context.Background(), "Bearer "+mint("org-a", "org-a"), "cycle-1")
		var he *HTTPError
		if !errors.As(err, &he) {
			return 0, "", err
		}
		return he.Status, he.Message, err
	}

	t.Run("open cycle → caller", func(t *testing.T) {
		a := NewRunnerAuthorizer(verifier, func(context.Context, string) (RunnerCycle, error) { return openCycle("org-a"), nil })
		caller, err := a.Authorize(context.Background(), "Bearer "+mint("org-a", "org-a"), "cycle-1")
		if err != nil || caller.Org != tenant.OrgHandle("org-a") {
			t.Fatalf("caller %+v err %v", caller, err)
		}
	})

	t.Run("closed cycle of the token's org → 403, logged value-free", func(t *testing.T) {
		logs := captureRunnerLogs(t)
		status, msg, err := answer(func(context.Context, string) (RunnerCycle, error) {
			return RunnerCycle{OrgHandle: "org-a"}, nil
		})
		if status != 403 || msg != cycleUnavailable {
			t.Fatalf("got %d %q (err %v), want 403 %q", status, msg, err, cycleUnavailable)
		}
		got := logsWithMsg(*logs, "runner callback: cycle closed")
		if len(got) != 1 || !reflect.DeepEqual(got[0], map[string]string{"cycle": "cycle-1"}) {
			t.Fatalf("closed-cycle log = %+v, want one with only the cycle id", got)
		}
	})

	t.Run("closed, missing and foreign cycles answer identically", func(t *testing.T) {
		closedS, closedM, _ := answer(func(context.Context, string) (RunnerCycle, error) { return RunnerCycle{OrgHandle: "org-a"}, nil })
		missingS, missingM, _ := answer(func(context.Context, string) (RunnerCycle, error) { return RunnerCycle{}, errors.New("no such cycle") })
		otherS, otherM, _ := answer(func(context.Context, string) (RunnerCycle, error) { return RunnerCycle{OrgHandle: "org-b"}, nil })
		if closedS != missingS || closedM != missingM || closedS != otherS || closedM != otherM {
			t.Fatalf("answers differ: closed=%d/%q missing=%d/%q other=%d/%q", closedS, closedM, missingS, missingM, otherS, otherM)
		}
	})

	t.Run("another org's closed cycle is an org mismatch first", func(t *testing.T) {
		logs := captureRunnerLogs(t)
		status, _, _ := answer(func(context.Context, string) (RunnerCycle, error) { return RunnerCycle{OrgHandle: "org-b"}, nil })
		if status != 403 {
			t.Fatalf("status = %d", status)
		}
		if len(logsWithMsg(*logs, "runner callback: cycle closed")) != 0 || len(logsWithMsg(*logs, "runner callback: publisher org mismatch")) != 1 {
			t.Fatalf("the org check must run first: %+v", *logs)
		}
	})
}

// runnerLog is one captured slog record: its message and attributes.
type runnerLog struct {
	msg   string
	attrs map[string]string
}

type runnerLogHandler struct{ records *[]runnerLog }

func (h runnerLogHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h runnerLogHandler) Handle(_ context.Context, r slog.Record) error {
	attrs := map[string]string{}
	r.Attrs(func(a slog.Attr) bool { attrs[a.Key] = a.Value.String(); return true })
	*h.records = append(*h.records, runnerLog{msg: r.Message, attrs: attrs})
	return nil
}
func (h runnerLogHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h runnerLogHandler) WithGroup(string) slog.Handler      { return h }

// captureRunnerLogs routes the default logger into a slice for the test.
func captureRunnerLogs(t *testing.T) *[]runnerLog {
	t.Helper()
	var records []runnerLog
	prev := slog.Default()
	slog.SetDefault(slog.New(runnerLogHandler{records: &records}))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &records
}

// logsWithMsg returns the attributes of every record with message msg.
func logsWithMsg(records []runnerLog, msg string) []map[string]string {
	var out []map[string]string
	for _, r := range records {
		if r.msg == msg {
			out = append(out, r.attrs)
		}
	}
	return out
}
