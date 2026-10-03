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
	"bytes"
	"context"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"
)

// multipartBody is a files upload of the named parts.
func multipartBody(t *testing.T, files map[string]string) (string, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	slices.Sort(names)
	for _, n := range names {
		fw, err := mw.CreateFormFile("files", n)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.WriteString(fw, files[n])
	}
	_ = mw.Close()
	return mw.FormDataContentType(), &buf
}

// partNames reads a multipart request and returns its files' names.
func partNames(t *testing.T, r *http.Request) []string {
	t.Helper()
	mr, err := r.MultipartReader()
	if err != nil {
		t.Errorf("not multipart: %v", err)
		return nil
	}
	var names []string
	for {
		p, err := mr.NextPart()
		if err != nil {
			break
		}
		names = append(names, p.FileName())
		_, _ = io.Copy(io.Discard, p)
	}
	return names
}

func TestPutReferences_StreamsTheUpload(t *testing.T) {
	var names []string
	var org, method, path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path, org = r.Method, r.URL.Path, r.Header.Get("X-Impersonate-Org")
		names = partNames(t, r)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	a := newAdapter(t, fixedTarget(srv.URL, "ou-123"), &countingTokens{})
	ct, body := multipartBody(t, map[string]string{"brief.pdf": "%PDF", "sketch.png": "png"})
	if err := a.PutReferences(context.Background(), acmeGreeter, ct, body); err != nil {
		t.Fatal(err)
	}
	if method != http.MethodPut || path != "/repos/acme/greeter/references" || org != "ou-123" {
		t.Fatalf("request = %s %s org=%s", method, path, org)
	}
	if !slices.Equal(names, []string{"brief.pdf", "sketch.png"}) {
		t.Fatalf("parts = %v", names)
	}
}

func TestPutReferences_Refusals(t *testing.T) {
	for _, tc := range []struct {
		name      string
		status    int
		code      string
		want      error
		permanent bool
	}{
		{name: "reference_rejected", status: 400, code: "reference_rejected", want: ErrReferenceRejected, permanent: true},
		{name: "disk_full", status: 503, code: "disk_full", want: ErrAEStudioUnavailable},
		{name: "owner_refused", status: 403, code: "owner_refused", permanent: true},
		{name: "payload_too_large", status: 413, code: "payload_too_large", permanent: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				_, _ = io.Copy(io.Discard, r.Body)
				writeProblem(w, tc.status, tc.code, "x.exe: type not allowed")
			}))
			defer srv.Close()
			logs := captureSlog(t)
			tok := &countingTokens{}
			a := newAdapter(t, fixedTarget(srv.URL, "ou-123"), tok)
			ct, body := multipartBody(t, map[string]string{"x.exe": "MZ"})
			err := a.PutReferences(context.Background(), acmeGreeter, ct, body)
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			var se *StatusError
			if tc.want == nil && (!errors.As(err, &se) || se.Code != tc.code || se.Status != tc.status) {
				t.Fatalf("err = %v, want StatusError %d %s", err, tc.status, tc.code)
			}
			if IsPermanent(err) != tc.permanent {
				t.Fatalf("IsPermanent(%v) = %v, want %v", err, IsPermanent(err), tc.permanent)
			}
			if calls != 1 || tok.invalidations() != 0 || len(logs.events(t, "ae_studio.auth_failed")) != 0 {
				t.Fatalf("calls=%d invalidated=%d: %s is not an auth failure", calls, tok.invalidations(), tc.code)
			}
		})
	}
	t.Run("reference_rejected keeps the detail", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeProblem(w, 400, "reference_rejected", "x.exe: type not allowed")
		}))
		defer srv.Close()
		a := newAdapter(t, fixedTarget(srv.URL, "ou-123"), &countingTokens{})
		ct, body := multipartBody(t, map[string]string{"x.exe": "MZ"})
		if err := a.PutReferences(context.Background(), acmeGreeter, ct, body); !strings.Contains(err.Error(), "x.exe: type not allowed") {
			t.Fatalf("err = %v, want the pod's detail", err)
		}
	})
}

// A 401 answered before any byte of the upload was sent (Expect:
// 100-continue) is retried once with a fresh token, the same body.
func TestPutReferences_RetriesAnUnsentUploadAfter401(t *testing.T) {
	var names []string
	srv := identityServer(t, func(w http.ResponseWriter, r *http.Request, n int) {
		if n == 1 {
			writeProblem(w, http.StatusUnauthorized, "unauthorized", "") // before reading: no 100 Continue
			return
		}
		names = partNames(t, r)
		w.WriteHeader(http.StatusNoContent)
	})
	tok := &countingTokens{}
	a := newAdapter(t, fixedTarget(srv.URL, "ou-123"), tok)
	ct, body := multipartBody(t, map[string]string{"sketch.png": strings.Repeat("p", 256<<10)})
	if err := a.PutReferences(context.Background(), acmeGreeter, ct, body); err != nil {
		t.Fatal(err)
	}
	if tok.invalidations() != 1 || !slices.Equal(names, []string{"sketch.png"}) {
		t.Fatalf("invalidated=%d parts=%v, want one refresh and the whole upload on the retry", tok.invalidations(), names)
	}
}

// A 401 after the upload was read cannot be replayed (the body is a stream):
// the token is dropped and the call is a retryable unavailable, never a
// silently truncated retry.
func TestPutReferences_401AfterTheUploadWasSentIsUnavailable(t *testing.T) {
	calls := 0
	srv := identityServer(t, func(w http.ResponseWriter, r *http.Request, n int) {
		calls = n
		_, _ = io.Copy(io.Discard, r.Body)
		writeProblem(w, http.StatusUnauthorized, "unauthorized", "")
	})
	logs := captureSlog(t)
	tok := &countingTokens{}
	a := newAdapter(t, fixedTarget(srv.URL, "ou-123"), tok)
	ct, body := multipartBody(t, map[string]string{"sketch.png": "png"})
	err := a.PutReferences(context.Background(), acmeGreeter, ct, body)
	if !errors.Is(err, ErrAEStudioUnavailable) || IsPermanent(err) {
		t.Fatalf("err = %v, want a retryable ErrAEStudioUnavailable", err)
	}
	if calls != 1 || tok.invalidations() != 1 || len(logs.events(t, "ae_studio.auth_failed")) != 0 {
		t.Fatalf("calls=%d invalidated=%d, want one call and a dropped token", calls, tok.invalidations())
	}
}

// PutReferences owns the body: an io.Closer is closed on every return, so a
// producer writing into an io.Pipe is released even when no byte was read.
func TestPutReferences_ClosesTheBody(t *testing.T) {
	ep := &fixedEndpoints{err: ErrAEStudioAbsent}
	a := newAdapter(t, ep, &countingTokens{})
	pr, pw := io.Pipe()
	done := make(chan error, 1)
	go func() {
		_, err := pw.Write([]byte("--boundary"))
		done <- err
	}()
	if err := a.PutReferences(context.Background(), acmeGreeter, "multipart/form-data; boundary=boundary", pr); !errors.Is(err, ErrAEStudioAbsent) {
		t.Fatalf("err = %v", err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, io.ErrClosedPipe) {
			t.Fatalf("producer err = %v, want io.ErrClosedPipe", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the producer is still blocked: the body was not closed")
	}
}
