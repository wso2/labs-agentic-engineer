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

package github

// Envelope handling for the GraphQL transport, at the unit tier: a GraphQL
// response is 200 whether it succeeded or not, so what matters is that the two
// failure modes stay distinguishable from each other and from success.

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// newGraphQLFake is newFake's GraphQL sibling: it records the one request and
// returns the real client with WithGraphQLEndpoint pointed at the fake.
func newGraphQLFake(t *testing.T, status int, respBody string) (*Client, *capture) {
	t.Helper()
	cap := &capture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		cap.method = r.Method
		cap.escapedPath = r.URL.EscapedPath()
		cap.body = string(b)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, respBody)
	}))
	t.Cleanup(srv.Close)
	c := New(Config{APIBase: srv.URL, Token: staticToken("tok")})
	return c, cap
}

func TestGraphQL_DecodesDataAndSendsVariables(t *testing.T) {
	c, cap := newGraphQLFake(t, http.StatusOK, `{"data":{"answer":42}}`)

	var out struct {
		Answer int `json:"answer"`
	}
	err := c.graphQL(context.Background(), "query($x: Int!) { answer }",
		map[string]any{"x": 1}, &out)
	if err != nil {
		t.Fatalf("graphQL: %v", err)
	}
	if out.Answer != 42 {
		t.Fatalf("answer = %d, want 42", out.Answer)
	}
	if cap.method != http.MethodPost {
		t.Fatalf("method = %s, want POST", cap.method)
	}

	var sent struct {
		Query     string         `json:"query"`
		Variables map[string]any `json:"variables"`
	}
	if err := json.Unmarshal([]byte(cap.body), &sent); err != nil {
		t.Fatalf("request body not json: %v (%s)", err, cap.body)
	}
	if sent.Query != "query($x: Int!) { answer }" {
		t.Fatalf("query = %q", sent.Query)
	}
	if sent.Variables["x"] != float64(1) {
		t.Fatalf("variables = %v", sent.Variables)
	}
}

// A populated errors[] is a failure even at HTTP 200, and every entry survives
// so a caller can branch on the machine-readable type rather than a message.
func TestGraphQL_ErrorsEnvelopeBecomesTypedError(t *testing.T) {
	c, _ := newGraphQLFake(t, http.StatusOK,
		`{"errors":[{"type":"NOT_FOUND","message":"Could not resolve to a Repository","path":["repository"]},`+
			`{"type":"FORBIDDEN","message":"nope"}],"data":null}`)

	err := c.graphQL(context.Background(), "query { repository { id } }", nil, &struct{}{})
	var ge *GraphQLError
	if !errors.As(err, &ge) {
		t.Fatalf("err = %v (%T), want *GraphQLError", err, err)
	}
	if len(ge.Errors) != 2 {
		t.Fatalf("preserved %d errors, want both", len(ge.Errors))
	}
	if !IsGraphQLType(err, "NOT_FOUND") || !IsGraphQLType(err, "FORBIDDEN") {
		t.Fatalf("both types must match: %v", err)
	}
	if IsGraphQLType(err, "RATE_LIMITED") {
		t.Fatalf("matched a type that is not present: %v", err)
	}
	if msg := err.Error(); msg != "github graphql error: NOT_FOUND: Could not resolve to a Repository; FORBIDDEN: nope" {
		t.Fatalf("message = %q", msg)
	}
	if ge.Query != "query { repository { id } }" {
		t.Fatalf("query not carried for debugging: %q", ge.Query)
	}
}

// A non-200 is transport/auth, not a GraphQL error — it keeps the same typed
// shape as the REST paths so 401 stays distinguishable from 5xx.
func TestGraphQL_NonOKStatusIsHTTPStatusError(t *testing.T) {
	c, _ := newGraphQLFake(t, http.StatusUnauthorized, `{"message":"Bad credentials"}`)

	err := c.graphQL(context.Background(), "query { viewer { login } }", nil, nil)
	if !IsHTTPStatus(err, http.StatusUnauthorized) {
		t.Fatalf("err = %v (%T), want an HTTPStatusError carrying 401", err, err)
	}
	var ge *GraphQLError
	if errors.As(err, &ge) {
		t.Fatalf("a transport failure must not masquerade as a GraphQL error: %v", err)
	}
}

// An envelope with neither data nor errors is a protocol violation, not an
// empty success — decoding it into a zero value would look like "no issues".
func TestGraphQL_EmptyEnvelopeFails(t *testing.T) {
	c, _ := newGraphQLFake(t, http.StatusOK, `{}`)

	var out struct{}
	if err := c.graphQL(context.Background(), "query { x }", nil, &out); err == nil {
		t.Fatal("want an error for an envelope with neither data nor errors, got nil")
	}
}
