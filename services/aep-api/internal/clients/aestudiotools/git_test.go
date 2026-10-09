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

package aestudiotools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

var (
	sha40  = strings.Repeat("a", 40)
	blob40 = strings.Repeat("1", 40)
	// trunkRef is a project on a non-main default branch: every git op must
	// name it (carry: omitted, the pod reads main).
	trunkRef = RepoRef{Org: "default", Owner: "acme", Repo: "greeter", DefaultBranch: "trunk"}
)

const (
	// pngHead is the first bytes of a PNG file: not valid UTF-8 (0x89).
	pngHead = "\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\xff\xfe"
	badUTF8 = "ok\xc3\x28\xa0\xa1end"
)

// opCase is one adapter call against the stub pod: the request it must send
// and what it decodes from reply.
type opCase struct {
	name   string
	call   func(ctx context.Context, a *Adapter) (any, error)
	status int    // reply status; 0 = 200
	reply  string // reply JSON; "" = no body
	method string
	path   string // escaped
	query  string // url.Values.Encode form
	body   string // request JSON; "" = no body
	want   any
}

func runOps(t *testing.T, cases []opCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, srv := newPodStub(t, func(w http.ResponseWriter, _ *http.Request) {
				status := tc.status
				if status == 0 {
					status = http.StatusOK
				}
				if tc.reply == "" {
					w.WriteHeader(status)
					return
				}
				writeJSON(w, status, tc.reply)
			})
			a := newAdapter(t, fixedTarget(srv.URL, "ou-123"), &countingTokens{})
			got, err := tc.call(context.Background(), a)
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			reqs := p.requests()
			if len(reqs) != 1 {
				t.Fatalf("requests = %d, want 1", len(reqs))
			}
			r := reqs[0]
			if r.Method != tc.method || r.Path != tc.path || r.Query != tc.query || r.ImpersonateOrg != "ou-123" {
				t.Fatalf("request = %s %s ?%s (org %s), want %s %s ?%s", r.Method, r.Path, r.Query, r.ImpersonateOrg, tc.method, tc.path, tc.query)
			}
			if !sameJSON(t, r.Body, tc.body) {
				t.Fatalf("body = %s, want %s", r.Body, tc.body)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("result = %#v, want %#v", got, tc.want)
			}
		})
	}
}

func sameJSON(t *testing.T, got, want string) bool {
	t.Helper()
	if want == "" {
		return got == ""
	}
	var g, w any
	if err := json.Unmarshal([]byte(got), &g); err != nil {
		return false
	}
	if err := json.Unmarshal([]byte(want), &w); err != nil {
		t.Fatalf("bad want JSON %s: %v", want, err)
	}
	return reflect.DeepEqual(g, w)
}

// pair carries a two-value result through opCase.want.
type pair struct {
	A, B any
}

func TestGitOps_RequestAndReply(t *testing.T) {
	created := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	runOps(t, []opCase{
		{
			name: "head local", method: "GET", path: "/repos/acme/greeter/head", query: "defaultBranch=trunk&local=true",
			reply: `{"sha":"` + sha40 + `"}`, want: sha40,
			call: func(ctx context.Context, a *Adapter) (any, error) {
				return a.Head(ctx, trunkRef, "", sourcecontrol.Local())
			},
		},
		{
			name: "head at a tag", method: "GET", path: "/repos/acme/greeter/head", query: "at=tags%2Fv1&defaultBranch=trunk",
			reply: `{"sha":"` + sha40 + `"}`, want: sha40,
			call: func(ctx context.Context, a *Adapter) (any, error) { return a.Head(ctx, trunkRef, "tags/v1") },
		},
		{
			name: "list tree", method: "GET", path: "/repos/acme/greeter/tree", query: "defaultBranch=trunk",
			reply: `{"commitSha":"` + sha40 + `","entries":[{"path":"specs/a.md","sha":"` + blob40 + `","size":3}]}`,
			want:  pair{[]sourcecontrol.Entry{{Path: "specs/a.md", SHA: blob40, Size: 3}}, sha40},
			call: func(ctx context.Context, a *Adapter) (any, error) {
				e, s, err := a.List(ctx, trunkRef, "")
				return pair{e, s}, err
			},
		},
		{
			name: "read file keeps slashes", method: "GET", path: "/repos/acme/greeter/files/specs/a%20b.md", query: "at=" + sha40 + "&defaultBranch=trunk",
			reply: `{"commitSha":"` + sha40 + `","path":"specs/a b.md","sha":"` + blob40 + `","content":"aGk="}`,
			want:  pair{[]byte("hi"), blob40},
			call: func(ctx context.Context, a *Adapter) (any, error) {
				c, s, err := a.ReadFile(ctx, trunkRef, sha40, "specs/a b.md")
				return pair{c, s}, err
			},
		},
		{
			name: "read bundle by prefix and ext", method: "GET", path: "/repos/acme/greeter/bundle",
			query: "at=tags%2Fv1&defaultBranch=trunk&ext=.md&ext=.yaml&prefix=specs%2F",
			reply: `{"commitSha":"` + sha40 + `","files":{"specs/a.md":"IyBB"}}`,
			want:  pair{map[string]string{"specs/a.md": "# A"}, sha40},
			call: func(ctx context.Context, a *Adapter) (any, error) {
				f, s, err := a.ReadBundle(ctx, trunkRef, "tags/v1", sourcecontrol.BundleFilter{Prefix: "specs/", Exts: []string{".md", ".yaml"}})
				return pair{f, s}, err
			},
		},
		{
			name: "read bundle by paths, local", method: "GET", path: "/repos/acme/greeter/bundle",
			query: "defaultBranch=trunk&local=true&path=a.md&path=b.md",
			reply: `{"commitSha":"` + sha40 + `","files":{}}`,
			want:  pair{map[string]string{}, sha40},
			call: func(ctx context.Context, a *Adapter) (any, error) {
				f, s, err := a.ReadBundle(ctx, trunkRef, "", sourcecontrol.BundleFilter{Paths: []string{"a.md", "b.md"}}, sourcecontrol.Local())
				return pair{f, s}, err
			},
		},
		{
			// Each file is base64 on the wire; binary bytes (PNG, invalid
			// UTF-8) come back byte for byte.
			name: "read bundle keeps binary files", method: "GET", path: "/repos/acme/greeter/bundle",
			query: "defaultBranch=trunk&prefix=skills%2F",
			reply: `{"commitSha":"` + sha40 + `","files":{"skills/a/logo.png":"` + base64.StdEncoding.EncodeToString([]byte(pngHead)) +
				`","skills/a/raw.bin":"` + base64.StdEncoding.EncodeToString([]byte(badUTF8)) + `"}}`,
			want: pair{map[string]string{"skills/a/logo.png": pngHead, "skills/a/raw.bin": badUTF8}, sha40},
			call: func(ctx context.Context, a *Adapter) (any, error) {
				f, s, err := a.ReadBundle(ctx, trunkRef, "", sourcecontrol.BundleFilter{Prefix: "skills/"})
				return pair{f, s}, err
			},
		},
		{
			name: "list tags", method: "GET", path: "/repos/acme/greeter/tags", query: "local=true&prefix=v",
			reply: `{"tags":[{"name":"v1","commitHash":"` + sha40 + `","message":"rel","body":"Features: F1 F2\nHeld back: F2.4","createdAt":"2026-01-02T03:04:05Z"},{"name":"v0","commitHash":"` + sha40 + `"}]}`,
			want: []sourcecontrol.TagInfo{
				{Name: "v1", CommitHash: sha40, Message: "rel", Body: "Features: F1 F2\nHeld back: F2.4", CreatedAt: created},
				{Name: "v0", CommitHash: sha40},
			},
			call: func(ctx context.Context, a *Adapter) (any, error) {
				return a.ListTags(ctx, trunkRef, "v", sourcecontrol.Local())
			},
		},
		{
			name: "create tag", method: "POST", path: "/repos/acme/greeter/tags", query: "defaultBranch=trunk",
			status: 201, reply: `{}`,
			body: `{"name":"v2","target":"` + sha40 + `","message":"rel","tagger":{"name":"Ann","email":"ann@x"}}`,
			want: nil,
			call: func(ctx context.Context, a *Adapter) (any, error) {
				return nil, a.Tag(ctx, trunkRef, sourcecontrol.TagSpec{Name: "v2", Target: sha40, Message: "rel", Tagger: &sourcecontrol.GitIdentity{Name: "Ann", Email: "ann@x"}})
			},
		},
		{
			name: "commit", method: "POST", path: "/repos/acme/greeter/commits", query: "defaultBranch=trunk",
			body: `{"message":"m","writes":[{"path":"a","content":"aGk=","baseSha":""}],"deletes":[{"path":"b","baseSha":"` + blob40 + `"}],` +
				`"author":{"name":"Ann","email":"ann@x"},"committer":{"name":"Bot","email":"bot@x"}}`,
			reply: `{"commitSha":"` + sha40 + `","changed":true,"files":[{"path":"a","sha":"` + blob40 + `"}],"warnings":[{"path":"a","code":"c","message":"w"}]}`,
			want: sourcecontrol.CommitResult{CommitSHA: sha40, Changed: true, Files: []sourcecontrol.CommittedFile{{Path: "a", SHA: blob40}},
				Warnings: []sourcecontrol.CommitWarning{{Path: "a", Code: "c", Message: "w"}}},
			call: func(ctx context.Context, a *Adapter) (any, error) {
				return a.Commit(ctx, trunkRef, sourcecontrol.CommitRequest{
					Message: "m", Writes: []sourcecontrol.FileWrite{{Path: "a", Content: "hi"}}, Deletes: []sourcecontrol.FileDelete{{Path: "b", BaseSHA: blob40}},
					Author: &sourcecontrol.GitIdentity{Name: "Ann", Email: "ann@x"}, Committer: &sourcecontrol.GitIdentity{Name: "Bot", Email: "bot@x"},
				})
			},
		},
		{
			name: "commit without identities", method: "POST", path: "/repos/acme/greeter/commits", query: "defaultBranch=trunk",
			body:  `{"message":"m","writes":[{"path":"a","content":"aGk=","baseSha":"` + blob40 + `"}]}`,
			reply: `{"commitSha":"` + sha40 + `","changed":false,"files":[],"warnings":[]}`,
			want:  sourcecontrol.CommitResult{CommitSHA: sha40},
			call: func(ctx context.Context, a *Adapter) (any, error) {
				return a.Commit(ctx, trunkRef, sourcecontrol.CommitRequest{Message: "m", Writes: []sourcecontrol.FileWrite{{Path: "a", Content: "hi", BaseSHA: blob40}}})
			},
		},
	})
}

func TestCommit_ConflictNamesThePaths(t *testing.T) {
	_, srv := newPodStub(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"type":"about:blank","title":"Conflict","status":409,"code":"conflict","conflicts":[{"path":"a","baseSha":"` + blob40 + `","currentSha":""}]}`))
	})
	a := newAdapter(t, fixedTarget(srv.URL, "ou-123"), &countingTokens{})
	_, err := a.Commit(context.Background(), trunkRef, sourcecontrol.CommitRequest{Message: "m", Writes: []sourcecontrol.FileWrite{{Path: "a", Content: "x", BaseSHA: blob40}}})
	var cc *sourcecontrol.CommitConflictError
	if !errors.As(err, &cc) || !reflect.DeepEqual(cc.Conflicts, []sourcecontrol.Conflict{{Path: "a", BaseSHA: blob40}}) {
		t.Fatalf("err = %v, want a CommitConflictError naming a", err)
	}
}

func TestGitOps_RefuseAnIncompleteRefWithoutACall(t *testing.T) {
	p, srv := newPodStub(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })
	a := newAdapter(t, fixedTarget(srv.URL, "ou-123"), &countingTokens{})
	if _, err := a.Head(context.Background(), RepoRef{Org: "default", Owner: "acme"}, ""); err == nil {
		t.Fatal("want an error for a ref without a repo")
	}
	if len(p.requests()) != 0 {
		t.Fatal("no request may leave for an incomplete ref")
	}
}

func TestGitOps_RateLimitCarriesRetryAfter(t *testing.T) {
	_, srv := newPodStub(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "7")
		writeProblem(w, http.StatusTooManyRequests, "github_rate_limited", "")
	})
	a := newAdapter(t, fixedTarget(srv.URL, "ou-123"), &countingTokens{})
	_, err := a.Head(context.Background(), trunkRef, "")
	var rl *sourcecontrol.RateLimitedError
	if !errors.As(err, &rl) || rl.RetryAfter != 7*time.Second {
		t.Fatalf("err = %v, want RateLimitedError{7s}", err)
	}
}

// read-file has no local form: a Local() read of the tip resolves the
// mirror's tip without a fetch, then reads at that sha.
func TestReadFile_LocalReadsAtTheMirrorsTip(t *testing.T) {
	p, srv := newPodStub(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/head") {
			writeJSON(w, 200, `{"sha":"`+sha40+`"}`)
			return
		}
		writeJSON(w, 200, `{"commitSha":"`+sha40+`","path":"a","sha":"`+blob40+`","content":"aGk="}`)
	})
	a := newAdapter(t, fixedTarget(srv.URL, "ou-123"), &countingTokens{})
	content, blob, err := a.ReadFile(context.Background(), trunkRef, "", "a", sourcecontrol.Local())
	if err != nil || string(content) != "hi" || blob != blob40 {
		t.Fatalf("ReadFile = %q %s %v", content, blob, err)
	}
	reqs := p.requests()
	if len(reqs) != 2 || reqs[0].Query != "defaultBranch=trunk&local=true" || reqs[1].Query != "at="+sha40+"&defaultBranch=trunk" {
		t.Fatalf("requests = %+v, want head?local then read-file at the sha", reqs)
	}
}
