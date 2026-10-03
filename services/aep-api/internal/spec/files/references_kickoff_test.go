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

package files

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/clients/aestudiotools"
	"github.com/wso2/aep/aep-api/internal/clients/aestudiotools/aestudiotest"
	"github.com/wso2/aep/aep-api/internal/gen"
	"github.com/wso2/aep/aep-api/internal/platform/apierr"
	"github.com/wso2/aep/aep-api/internal/platform/tenant"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// UNIT tier for the reference upload pass-through over the in-memory pod
// (aestudiotest.Fake): the parts are re-streamed to the org's pod, which
// stores them, and the held kickoff fires only on the pod's 2xx.

var pngBytes = []byte("\x89PNG\r\n\x1a\n not really a png")

// kickoffSpy counts the kickoffs the upload releases.
type kickoffSpy struct{ calls int }

func (k *kickoffSpy) Kickoff(context.Context, string, string) { k.calls++ }

// repoRows serves one project's repository row.
type repoRows struct{ row *sourcecontrol.GitRepository }

func (r repoRows) GetRepo(_ context.Context, org, project string) (*sourcecontrol.GitRepository, error) {
	if r.row == nil || r.row.OrgID != org || r.row.ProjectID != project {
		return nil, sourcecontrol.ErrRepoNotFound
	}
	return r.row, nil
}

func memRepos(t *testing.T, org, project, url string) repoRows {
	t.Helper()
	return repoRows{row: &sourcecontrol.GitRepository{OrgID: org, ProjectID: project, RepoURL: url}}
}

func tenantCtx(org string) context.Context {
	return tenant.WithBoundOrg(context.Background(), org)
}

// multipartRequest is the strict server's view of an upload of project "p":
// a multipart reader over field `files`, one part per document, in name order.
func multipartRequest(t *testing.T, docs map[string][]byte) gen.PutProjectReferencesRequestObject {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	names := make([]string, 0, len(docs))
	for n := range docs {
		names = append(names, n)
	}
	slices.Sort(names)
	for _, n := range names {
		part, err := w.CreateFormFile("files", n)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(docs[n]); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return gen.PutProjectReferencesRequestObject{ProjectName: "p", Body: multipart.NewReader(&buf, w.Boundary())}
}

func TestPutProjectReferences_StreamsThenKicksOff(t *testing.T) {
	f := aestudiotest.New()
	kick := &kickoffSpy{}
	h := NewHandler(f, memRepos(t, "default", "p", "https://github.com/acme/greeter"), kick)
	req := multipartRequest(t, map[string][]byte{"sketch.png": pngBytes}) // gen.PutProjectReferencesRequestObject
	if _, err := h.PutProjectReferences(tenantCtx("default"), req); err != nil {
		t.Fatal(err)
	}
	if got := f.References(aestudiotools.RepoRef{Org: "default", Owner: "acme", Repo: "greeter"}); !slices.Equal(got, []string{"sketch.png"}) {
		t.Fatalf("pod got %v", got)
	}
	if kick.calls != 1 {
		t.Fatal("kickoff must fire after a 2xx")
	}
	f.FailOp("put-references", aestudiotools.ErrReferenceRejected)
	_, err := h.PutProjectReferences(tenantCtx("default"), multipartRequest(t, map[string][]byte{"x.exe": {1}}))
	var ae *apierr.Error
	if !errors.As(err, &ae) || ae.Status != 400 || kick.calls != 1 {
		t.Fatalf("err=%v kicks=%d, want 400 and no second kickoff", err, kick.calls)
	}
}

// Every part reaches the pod under field `files`, with its own file name and
// content type, in upload order.
func TestPutProjectReferences_KeepsEveryPartsNameAndType(t *testing.T) {
	rec := &recordingRefs{}
	h := NewHandler(rec, memRepos(t, "default", "p", "https://github.com/acme/greeter"), nil)

	if _, err := h.PutProjectReferences(tenantCtx("default"), multipartRequest(t, map[string][]byte{
		"a-brief.md": []byte("# brief"), "b-sketch.png": pngBytes,
	})); err != nil {
		t.Fatal(err)
	}
	want := []recordedPart{
		{field: "files", name: "a-brief.md", contentType: "application/octet-stream", body: "# brief"},
		{field: "files", name: "b-sketch.png", contentType: "application/octet-stream", body: string(pngBytes)},
	}
	if !slices.Equal(rec.parts, want) {
		t.Fatalf("pod got %+v, want %+v", rec.parts, want)
	}
}

// No part is buffered whole: the pod reads the first bytes of a document
// while the client is still sending it (R21). A handler that buffered the
// part would deadlock here, because the client sends the rest only after the
// pod has seen the start.
func TestPutProjectReferences_StreamsEachPartWithoutBuffering(t *testing.T) {
	sawFirstChunk := make(chan struct{})
	refs := &streamingRefs{firstChunk: "chunk-one|", sawFirstChunk: sawFirstChunk}
	h := NewHandler(refs, memRepos(t, "default", "p", "https://github.com/acme/greeter"), nil)

	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	go func() {
		part, err := mw.CreateFormFile("files", "big.pdf")
		if err != nil {
			_ = pw.CloseWithError(err)
			return
		}
		_, _ = part.Write([]byte("chunk-one|"))
		select {
		case <-sawFirstChunk:
		case <-time.After(5 * time.Second):
			_ = pw.CloseWithError(errors.New("the pod never saw the first chunk: the part was buffered"))
			return
		}
		_, _ = part.Write([]byte("chunk-two"))
		_ = pw.CloseWithError(mw.Close())
	}()

	req := gen.PutProjectReferencesRequestObject{ProjectName: "p", Body: multipart.NewReader(pr, mw.Boundary())}
	if _, err := h.PutProjectReferences(tenantCtx("default"), req); err != nil {
		t.Fatal(err)
	}
	if refs.body != "chunk-one|chunk-two" {
		t.Fatalf("pod got %q", refs.body)
	}
}

// A client that breaks off mid-upload aborts the pod's upload too: the pipe
// is closed with the copy error, so the pod never stores a truncated set,
// and the caller is told its body was bad.
func TestPutProjectReferences_ABrokenUploadAbortsThePodUpload(t *testing.T) {
	f := aestudiotest.New()
	kick := &kickoffSpy{}
	h := NewHandler(f, memRepos(t, "default", "p", "https://github.com/acme/greeter"), kick)

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	part, _ := mw.CreateFormFile("files", "cut.pdf")
	_, _ = part.Write([]byte("half a document"))
	// No closing boundary: the body ends mid-part.
	req := gen.PutProjectReferencesRequestObject{ProjectName: "p", Body: multipart.NewReader(&buf, mw.Boundary())}

	_, err := h.PutProjectReferences(tenantCtx("default"), req)
	var ae *apierr.Error
	if !errors.As(err, &ae) || ae.Status != http.StatusBadRequest {
		t.Fatalf("err = %v, want a 400", err)
	}
	if got := f.References(aestudiotools.RepoRef{Org: "default", Owner: "acme", Repo: "greeter"}); got != nil {
		t.Fatalf("pod stored %v from a broken upload", got)
	}
	if kick.calls != 0 {
		t.Fatal("a failed upload must not fire the kickoff")
	}
}

// What the pod's refusals mean to the caller. No kickoff fires on any of them.
func TestPutProjectReferences_MapsThePodsAnswers(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{"rejected", fmt.Errorf("%w: x.exe: not an allowed type", aestudiotools.ErrReferenceRejected), http.StatusBadRequest, apierr.CodeBadRequest},
		// C3: an operator fault, not a blip: 503 with its own code and no Retry-After.
		{"misconfigured", aestudiotools.ErrAEStudioMisconfigured, http.StatusServiceUnavailable, "ae_studio_misconfigured"},
		{"unavailable", aestudiotools.ErrAEStudioUnavailable, http.StatusServiceUnavailable, "ae_studio_unavailable"},
		{"absent", aestudiotools.ErrAEStudioAbsent, http.StatusConflict, "github_not_connected"},
		{"too large", &aestudiotools.StatusError{Op: "put-repo-references", Status: http.StatusRequestEntityTooLarge}, http.StatusRequestEntityTooLarge, "request_too_large"},
		{"anything else", &aestudiotools.StatusError{Op: "put-repo-references", Status: http.StatusForbidden, Code: "owner_refused"}, http.StatusBadGateway, apierr.CodeBadGateway},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := aestudiotest.New()
			f.FailOp(aestudiotest.OpPutReferences, tc.err)
			kick := &kickoffSpy{}
			h := NewHandler(f, memRepos(t, "default", "p", "https://github.com/acme/greeter"), kick)

			_, err := h.PutProjectReferences(tenantCtx("default"), multipartRequest(t, map[string][]byte{"a.md": []byte("a")}))
			var ae *apierr.Error
			if !errors.As(err, &ae) || ae.Status != tc.wantStatus || ae.Code != tc.wantCode {
				t.Fatalf("err = %#v, want %d %s", err, tc.wantStatus, tc.wantCode)
			}
			if kick.calls != 0 {
				t.Fatal("a refused upload must not fire the kickoff")
			}
		})
	}
}

// The project resolves inside the caller's org: another org's project, or
// none, is a 404 and nothing reaches any pod.
func TestPutProjectReferences_UnknownProjectIs404(t *testing.T) {
	f := aestudiotest.New()
	h := NewHandler(f, memRepos(t, "default", "p", "https://github.com/acme/greeter"), &kickoffSpy{})

	_, err := h.PutProjectReferences(tenantCtx("evil"), multipartRequest(t, map[string][]byte{"a.md": []byte("a")}))
	var ae *apierr.Error
	if !errors.As(err, &ae) || ae.Status != http.StatusNotFound {
		t.Fatalf("err = %v, want 404", err)
	}
	if got := f.References(aestudiotools.RepoRef{Org: "evil", Owner: "acme", Repo: "greeter"}); got != nil {
		t.Fatalf("pod stored %v for a foreign project", got)
	}
}

// An unwired starter is a documented no-op: the upload still reaches the pod.
func TestPutProjectReferences_NilKickoffStarterIsNoOp(t *testing.T) {
	f := aestudiotest.New()
	h := NewHandler(f, memRepos(t, "default", "p", "https://github.com/acme/greeter"), nil)

	if _, err := h.PutProjectReferences(tenantCtx("default"), multipartRequest(t, map[string][]byte{"a.md": []byte("a")})); err != nil {
		t.Fatal(err)
	}
	if got := f.References(aestudiotools.RepoRef{Org: "default", Owner: "acme", Repo: "greeter"}); !slices.Equal(got, []string{"a.md"}) {
		t.Fatalf("pod got %v", got)
	}
}

type recordedPart struct{ field, name, contentType, body string }

// recordingRefs is a pod that records every part it is sent.
type recordingRefs struct{ parts []recordedPart }

func (r *recordingRefs) PutReferences(_ context.Context, _ aestudiotools.RepoRef, contentType string, body io.Reader) error {
	_, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return err
	}
	mr := multipart.NewReader(body, params["boundary"])
	for {
		p, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		b, err := io.ReadAll(p)
		if err != nil {
			return err
		}
		r.parts = append(r.parts, recordedPart{field: p.FormName(), name: p.FileName(), contentType: p.Header.Get("Content-Type"), body: string(b)})
	}
}

// streamingRefs is a pod that signals once it has read firstChunk of the
// first part, then reads the rest.
type streamingRefs struct {
	firstChunk    string
	sawFirstChunk chan struct{}
	body          string
}

func (s *streamingRefs) PutReferences(_ context.Context, _ aestudiotools.RepoRef, contentType string, body io.Reader) error {
	_, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return err
	}
	p, err := multipart.NewReader(body, params["boundary"]).NextPart()
	if err != nil {
		return err
	}
	head := make([]byte, len(s.firstChunk))
	if _, err := io.ReadFull(p, head); err != nil {
		return err
	}
	close(s.sawFirstChunk)
	rest, err := io.ReadAll(p)
	if err != nil {
		return err
	}
	s.body = string(head) + string(rest)
	return nil
}
