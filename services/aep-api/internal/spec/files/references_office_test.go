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
	"archive/zip"
	"bytes"
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/clients/aestudiotools"
	"github.com/wso2/aep/aep-api/internal/clients/aestudiotools/aestudiotest"
	"github.com/wso2/aep/aep-api/internal/platform/apierr"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// UNIT tier for the Office conversion on the reference upload (#878 S5): the
// models do not read Office formats and the pod does not store them, so an
// Office part is converted to markdown here and streamed as `<name>.md`.

var greeterRef = aestudiotools.RepoRef{Org: "default", Owner: "acme", Repo: "greeter"}

// wordDocument is a minimal .docx: one paragraph.
func wordDocument(t *testing.T, text string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	f, err := zw.Create("word/document.xml")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.Write([]byte(`<w:document xmlns:w="w"><w:body><w:p><w:r><w:t>` + text + `</w:t></w:r></w:p></w:body></w:document>`))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// Port of main's TestPutReferences_OfficeDocumentIsStoredAsMarkdown: an Office
// document is converted on upload and stored by its own name plus .md; one
// that is not really an Office file is refused with 400.
func TestPutReferences_OfficeDocumentIsStoredAsMarkdown(t *testing.T) {
	f := aestudiotest.New()
	h := NewHandler(f, memRepos(t, "default", "p", "https://github.com/acme/greeter"), nil)

	if _, err := h.PutProjectReferences(tenantCtx("default"), multipartRequest(t, map[string][]byte{
		"policy.docx": wordDocument(t, "Receipts above $25."),
	})); err != nil {
		t.Fatal(err)
	}
	names, _ := f.ListReferences(t.Context(), greeterRef)
	if !slices.Equal(names, []string{"policy.docx.md"}) {
		t.Fatalf("stored %v, want [policy.docx.md]", names)
	}

	_, err := h.PutProjectReferences(tenantCtx("default"), multipartRequest(t, map[string][]byte{"broken.xlsx": []byte("not a zip")}))
	var ae *apierr.Error
	if !errors.As(err, &ae) || ae.Status != http.StatusBadRequest {
		t.Fatalf("a broken workbook got %v, want 400", err)
	}
	if want := `"broken.xlsx" could not be read as a .xlsx file`; ae.Message != want {
		t.Fatalf("message = %q, want %q", ae.Message, want)
	}
}

// The pod receives the converted words, not the Office bytes: a markdown part
// named `<name>.md`, beside the other parts as they came.
func TestPutReferences_OfficePartReachesThePodAsMarkdown(t *testing.T) {
	rec := &recordingRefs{}
	h := NewHandler(rec, memRepos(t, "default", "p", "https://github.com/acme/greeter"), nil)

	if _, err := h.PutProjectReferences(tenantCtx("default"), multipartRequest(t, map[string][]byte{
		"a-brief.md": []byte("# brief"), "b-Policy.DOCX": wordDocument(t, "Receipts above $25."),
	})); err != nil {
		t.Fatal(err)
	}
	if len(rec.parts) != 2 || rec.parts[0].name != "a-brief.md" || rec.parts[0].body != "# brief" {
		t.Fatalf("pod got %+v, want the brief untouched first", rec.parts)
	}
	doc := rec.parts[1]
	if doc.field != "files" || doc.name != "b-Policy.DOCX.md" || !strings.HasPrefix(doc.contentType, "text/markdown") {
		t.Fatalf("converted part = %+v, want files/b-Policy.DOCX.md as text/markdown", doc)
	}
	if !strings.Contains(doc.body, "Receipts above $25.") || strings.HasPrefix(doc.body, "PK") {
		t.Fatalf("converted body = %q, want the document's words", doc.body)
	}
}

// A failed conversion refuses the whole upload before the Office part reaches
// the pod, and the pod stores nothing, not even the parts sent before it.
func TestPutReferences_UnreadableOfficePartStoresNothing(t *testing.T) {
	f := aestudiotest.New()
	kick := &kickoffSpy{}
	h := NewHandler(f, memRepos(t, "default", "p", "https://github.com/acme/greeter"), kick)

	_, err := h.PutProjectReferences(tenantCtx("default"), multipartRequest(t, map[string][]byte{
		"a-brief.md": []byte("# brief"), "b-deck.pptx": []byte("not a zip"),
	}))
	var ae *apierr.Error
	if !errors.As(err, &ae) || ae.Status != http.StatusBadRequest {
		t.Fatalf("err = %v, want 400", err)
	}
	if got := f.References(greeterRef); got != nil {
		t.Fatalf("pod stored %v from a refused upload", got)
	}
	if kick.calls != 0 {
		t.Fatal("a refused upload must not fire the kickoff")
	}
}

// An Office part is held whole only up to the pod's per-document limit: one
// past it is refused with 400 before it is converted, and nothing is stored.
func TestPutReferences_OversizedOfficePartIs400(t *testing.T) {
	f := aestudiotest.New()
	h := NewHandler(f, memRepos(t, "default", "p", "https://github.com/acme/greeter"), nil)

	_, err := h.PutProjectReferences(tenantCtx("default"), multipartRequest(t, map[string][]byte{
		"big.docx": bytes.Repeat([]byte("a"), sourcecontrol.MaxReferenceBytes+1),
	}))
	var ae *apierr.Error
	if !errors.As(err, &ae) || ae.Status != http.StatusBadRequest {
		t.Fatalf("err = %v, want 400", err)
	}
	if want := `"big.docx" exceeds the 5 MiB per-document limit`; ae.Message != want {
		t.Fatalf("message = %q, want %q", ae.Message, want)
	}
	if got := f.References(greeterRef); got != nil {
		t.Fatalf("pod stored %v", got)
	}
}
