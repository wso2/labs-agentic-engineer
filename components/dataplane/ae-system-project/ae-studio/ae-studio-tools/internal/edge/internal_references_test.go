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

package edge

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/wso2/aep/ae-studio-tools/internal/repo"
)

const referencesPath = "/internal/v1/repos/acme-gh/greeter/references"

// refPart is one multipart part: a form field name, a file name and bytes.
type refPart struct {
	field, file string
	content     []byte
}

func refFile(name string, size int) refPart {
	return refPart{field: "files", file: name, content: bytes.Repeat([]byte("a"), size)}
}

// multipartBody encodes parts as multipart/form-data and returns the body
// and its Content-Type.
func multipartBody(t *testing.T, parts ...refPart) ([]byte, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for _, p := range parts {
		var w io.Writer
		var err error
		if p.file == "" {
			w, err = mw.CreateFormField(p.field)
		} else {
			w, err = mw.CreateFormFile(p.field, p.file)
		}
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(p.content); err != nil {
			t.Fatal(err)
		}
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes(), mw.FormDataContentType()
}

// putReferences sends parts to path as the given caller. A chunked request
// has an unknown length (ContentLength -1), so only the body reader caps it.
func (h *harness) putReferences(path, token string, chunked bool, parts ...refPart) *httptest.ResponseRecorder {
	h.t.Helper()
	body, contentType := multipartBody(h.t, parts...)
	var r *http.Request
	if chunked {
		r = httptest.NewRequest(http.MethodPut, path, struct{ io.Reader }{bytes.NewReader(body)})
	} else {
		r = httptest.NewRequest(http.MethodPut, path, bytes.NewReader(body))
	}
	r.Header.Set("Content-Type", contentType)
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	r.Header.Set("X-Impersonate-Org", "ou-1")
	rec := httptest.NewRecorder()
	h.handler.ServeHTTP(rec, r)
	return rec
}

func (h *harness) storedReferences(owner, name string) []string {
	h.t.Helper()
	got, err := h.engine.ListReferences(context.Background(), repo.OwnerRepo{Owner: owner, Repo: name})
	if err != nil {
		h.t.Fatal(err)
	}
	return got
}

// referencesStoreEmpty reports whether nothing at all was written under the
// studio's references/ dir.
func (h *harness) referencesStoreEmpty() bool {
	h.t.Helper()
	entries, err := os.ReadDir(repo.ReferencesDir(h.engine.Root()))
	if err != nil && !os.IsNotExist(err) {
		h.t.Fatal(err)
	}
	return len(entries) == 0
}

func wantProblem(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if rec.Code != status || !strings.Contains(rec.Body.String(), `"code":"`+code+`"`) {
		t.Fatalf("got %d %s, want %d %s", rec.Code, rec.Body.String(), status, code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Fatalf("Content-Type = %q", ct)
	}
}

func TestReferencesUpload(t *testing.T) {
	t.Run("three valid files are stored and listed sorted", func(t *testing.T) {
		h := newHarness(t)
		rec := h.putReferences(referencesPath, h.m2m(), false,
			refFile("notes.md", 10), refFile("brief.pdf", 20), refFile("api.txt", 30))
		if rec.Code != http.StatusNoContent {
			t.Fatalf("got %d %s", rec.Code, rec.Body.String())
		}
		if got, want := h.storedReferences("Acme-GH", "greeter"), []string{"api.txt", "brief.pdf", "notes.md"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("stored = %v, want %v", got, want)
		}
	})

	t.Run("a body over the default 1 MiB cap streams through", func(t *testing.T) {
		h := newHarness(t)
		for _, chunked := range []bool{false, true} {
			rec := h.putReferences(referencesPath, h.m2m(), chunked,
				refFile("a.pdf", 2<<20), refFile("b.pdf", 2<<20), refFile("c.pdf", 2<<20))
			if rec.Code != http.StatusNoContent {
				t.Fatalf("chunked=%v: got %d %s", chunked, rec.Code, rec.Body.String())
			}
		}
		if got := h.storedReferences("acme-gh", "greeter"); len(got) != 3 {
			t.Fatalf("stored = %v", got)
		}
	})

	t.Run("an upload replaces the whole set", func(t *testing.T) {
		h := newHarness(t)
		h.putReferences(referencesPath, h.m2m(), false, refFile("old.md", 1), refFile("keep.md", 1))
		rec := h.putReferences(referencesPath, h.m2m(), false, refFile("keep.md", 2))
		if rec.Code != http.StatusNoContent {
			t.Fatalf("got %d %s", rec.Code, rec.Body.String())
		}
		if got := h.storedReferences("acme-gh", "greeter"); !reflect.DeepEqual(got, []string{"keep.md"}) {
			t.Fatalf("stored = %v", got)
		}
	})

	t.Run("names are sanitized to a bare store-safe name", func(t *testing.T) {
		h := newHarness(t)
		rec := h.putReferences(referencesPath, h.m2m(), false,
			refFile("../x.pdf", 1), refFile(`..\..\Evil Plan.MD`, 1), refFile("My Notes!.txt", 1))
		if rec.Code != http.StatusNoContent {
			t.Fatalf("got %d %s", rec.Code, rec.Body.String())
		}
		if got, want := h.storedReferences("acme-gh", "greeter"), []string{"evil-plan.md", "my-notes.txt", "x.pdf"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("stored = %v, want %v", got, want)
		}
	})

	rejected := []struct {
		name  string
		parts []refPart
	}{
		{"eleven files", func() []refPart {
			var ps []refPart
			for i := range 11 {
				ps = append(ps, refFile(string(rune('a'+i))+".md", 1))
			}
			return ps
		}()},
		{"an executable", []refPart{refFile("tool.exe", 1)}},
		{"a file over 5 MiB", []refPart{refFile("big.pdf", 6<<20)}},
		{"two names that sanitize alike", []refPart{refFile("My Notes.md", 1), refFile("my-notes.md", 1)}},
		{"the same name twice", []refPart{refFile("a.md", 1), refFile("a.md", 1)}},
		{"an empty body", nil},
	}
	for _, c := range rejected {
		t.Run(c.name+" is reference_rejected and writes nothing", func(t *testing.T) {
			h := newHarness(t)
			rec := h.putReferences(referencesPath, h.m2m(), false, c.parts...)
			wantProblem(t, rec, http.StatusBadRequest, "reference_rejected")
			if !h.referencesStoreEmpty() {
				t.Fatal("a rejected upload wrote to the store")
			}
		})
	}

	t.Run("a rejected upload keeps the stored set", func(t *testing.T) {
		h := newHarness(t)
		h.putReferences(referencesPath, h.m2m(), false, refFile("keep.md", 1))
		rec := h.putReferences(referencesPath, h.m2m(), false, refFile("tool.exe", 1))
		wantProblem(t, rec, http.StatusBadRequest, "reference_rejected")
		if got := h.storedReferences("acme-gh", "greeter"); !reflect.DeepEqual(got, []string{"keep.md"}) {
			t.Fatalf("stored = %v", got)
		}
	})

	t.Run("a field other than files is validation_failed and writes nothing", func(t *testing.T) {
		h := newHarness(t)
		rec := h.putReferences(referencesPath, h.m2m(), false, refFile("a.md", 1), refPart{field: "other", content: []byte("x")})
		wantProblem(t, rec, http.StatusBadRequest, "validation_failed")
		if !h.referencesStoreEmpty() {
			t.Fatal("a malformed upload wrote to the store")
		}
	})

	t.Run("a body that is not multipart is validation_failed", func(t *testing.T) {
		h := newHarness(t)
		r := httptest.NewRequest(http.MethodPut, referencesPath, strings.NewReader(`{"files":[]}`))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", "Bearer "+h.m2m())
		r.Header.Set("X-Impersonate-Org", "ou-1")
		rec := httptest.NewRecorder()
		h.handler.ServeHTTP(rec, r)
		wantProblem(t, rec, http.StatusBadRequest, "validation_failed")
	})

	t.Run("a user JWT is 401 and writes nothing", func(t *testing.T) {
		h := newHarness(t)
		rec := h.putReferences(referencesPath, h.user("default", "ou-1"), false, refFile("a.md", 1))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("got %d %s", rec.Code, rec.Body.String())
		}
		if !h.referencesStoreEmpty() {
			t.Fatal("an unauthenticated upload wrote to the store")
		}
	})

	t.Run("an owner other than the connected account is refused and writes nothing", func(t *testing.T) {
		h := newHarness(t)
		rec := h.putReferences("/internal/v1/repos/someone-else/greeter/references", h.m2m(), false, refFile("a.md", 1))
		wantProblem(t, rec, http.StatusForbidden, "owner_refused")
		if !h.referencesStoreEmpty() {
			t.Fatal("a foreign owner's upload wrote to the store")
		}
	})

	t.Run("no connected owner refuses every upload", func(t *testing.T) {
		h := newHarness(t)
		h.handler = internalChain(internalBodyCaps, h.engine, "")
		rec := h.putReferences(referencesPath, h.m2m(), false, refFile("a.md", 1))
		wantProblem(t, rec, http.StatusForbidden, "owner_refused")
		if !h.referencesStoreEmpty() {
			t.Fatal("an upload with no connected owner wrote to the store")
		}
	})

	t.Run("usage at 90% is disk_full", func(t *testing.T) {
		h := newHarness(t)
		h.engine.SetUsageGauge(func() int { return repo.DiskAdmissionRefusePct })
		rec := h.putReferences(referencesPath, h.m2m(), false, refFile("a.md", 1))
		wantProblem(t, rec, http.StatusServiceUnavailable, "disk_full")
		if !h.referencesStoreEmpty() {
			t.Fatal("a refused upload wrote to the store")
		}
		line := h.logLine("files.disk_full")
		if line["op"] != "put-references" || line["repo"] != "acme-gh/greeter" {
			t.Fatalf("log line = %v", line)
		}
	})

	t.Run("a declared length over 80 MiB is 413 before the gate", func(t *testing.T) {
		h := newHarness(t)
		r := httptest.NewRequest(http.MethodPut, referencesPath, strings.NewReader("x"))
		r.ContentLength = referencesBodyBytes + 1
		rec := httptest.NewRecorder()
		h.handler.ServeHTTP(rec, r)
		wantProblem(t, rec, http.StatusRequestEntityTooLarge, "payload_too_large")
	})

	t.Run("an unknown-length body past the op cap is 413", func(t *testing.T) {
		h := newHarness(t)
		h.handler = internalChain(map[string]int64{"put-repo-references": 64 << 10}, h.engine, "acme-gh")
		rec := h.putReferences(referencesPath, h.m2m(), true, refFile("a.pdf", 100<<10))
		wantProblem(t, rec, http.StatusRequestEntityTooLarge, "payload_too_large")
		if !h.referencesStoreEmpty() {
			t.Fatal("a capped upload wrote to the store")
		}
	})
}

// TestInternalBodyCaps pins the per-op cap table: only the references upload
// and the turn start (the agent's own Turn-socket cap) are allowed more than
// the default.
func TestInternalBodyCaps(t *testing.T) {
	if want := map[string]int64{"put-repo-references": 80 << 20, "start-repo-turn": 4 << 20}; !reflect.DeepEqual(internalBodyCaps, want) {
		t.Fatalf("internalBodyCaps = %v, want %v", internalBodyCaps, want)
	}
	if internalBodyBytes != 1<<20 {
		t.Fatalf("internalBodyBytes = %d", internalBodyBytes)
	}
}

// internalChain is the /internal/v1 chain without the gate (cap table →
// validator → server), serving the reference store with the given connected
// owner.
func internalChain(caps map[string]int64, store ReferenceStore, owner string) http.Handler {
	server := internalServer{refs: store, githubOwner: owner}
	return capOpBody(internalRouteFinder, caps, internalBodyBytes, internalHandler(internalRouteFinder, server))
}
