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
	"log/slog"
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

// officeZip builds an Office file from its parts.
func officeZip(t *testing.T, parts map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range parts {
		f, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// amplifyingOfficeFiles are small uploads whose markdown would be far larger
// than the pod's per-document limit: the conversion's output, not only its
// input, is what must stay inside it.
func amplifyingOfficeFiles(t *testing.T) map[string][]byte {
	t.Helper()
	// Word: one paragraph of 6 MiB of one letter, which deflates to a few KiB.
	word := officeZip(t, map[string]string{"word/document.xml": `<w:document xmlns:w="w"><w:body><w:p><w:r><w:t>` +
		strings.Repeat("a", 6<<20) + `</w:t></w:r></w:p></w:body></w:document>`})
	// Excel: 1000 cells that each name one 64 KiB shared string.
	var sheet strings.Builder
	for r := 1; r <= 100; r++ {
		sheet.WriteString("<row>")
		for c := 0; c < 10; c++ {
			sheet.WriteString(`<c t="s"><v>0</v></c>`)
		}
		sheet.WriteString("</row>")
	}
	excel := officeZip(t, map[string]string{
		"xl/workbook.xml":            `<workbook xmlns:r="r"><sheets><sheet name="S" sheetId="1" r:id="rId1"/></sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": `<Relationships><Relationship Id="rId1" Target="worksheets/sheet1.xml"/></Relationships>`,
		"xl/sharedStrings.xml":       `<sst><si><t>` + strings.Repeat("b", 64<<10) + `</t></si></sst>`,
		"xl/worksheets/sheet1.xml":   `<worksheet><sheetData>` + sheet.String() + `</sheetData></worksheet>`,
	})
	// PowerPoint: 2000 deck entries that all name one 8 KiB slide.
	deck := strings.Repeat(`<p:sldId r:id="rId1"/>`, 2000)
	power := officeZip(t, map[string]string{
		"ppt/presentation.xml":            `<p:presentation xmlns:p="p" xmlns:r="r"><p:sldIdLst>` + deck + `</p:sldIdLst></p:presentation>`,
		"ppt/_rels/presentation.xml.rels": `<Relationships><Relationship Id="rId1" Target="slides/slide1.xml"/></Relationships>`,
		"ppt/slides/slide1.xml":           `<p:sld xmlns:p="p" xmlns:a="a"><a:p><a:r><a:t>` + strings.Repeat("c", 8<<10) + `</a:t></a:r></a:p></p:sld>`,
	})
	return map[string][]byte{"memo.docx": word, "rates.xlsx": excel, "deck.pptx": power}
}

// A small Office upload whose markdown would pass the pod's per-document
// limit is refused with that limit's 400 while it is converted, and no byte
// of it reaches the pod.
func TestPutReferences_AmplifyingOfficePartIs400(t *testing.T) {
	for name, content := range amplifyingOfficeFiles(t) {
		t.Run(name, func(t *testing.T) {
			if len(content) > 1<<20 {
				t.Fatalf("fixture is %d bytes, want a small upload", len(content))
			}
			rec := &recordingRefs{}
			h := NewHandler(rec, memRepos(t, "default", "p", "https://github.com/acme/greeter"), nil)

			_, err := h.PutProjectReferences(tenantCtx("default"), multipartRequest(t, map[string][]byte{name: content}))
			var ae *apierr.Error
			if !errors.As(err, &ae) || ae.Status != http.StatusBadRequest {
				t.Fatalf("err = %v, want 400", err)
			}
			if want := `"` + name + `" exceeds the 5 MiB per-document limit`; ae.Message != want {
				t.Fatalf("message = %q, want %q", ae.Message, want)
			}
			if len(rec.parts) != 0 {
				t.Fatalf("the pod got %d parts, want none", len(rec.parts))
			}
		})
	}
}

// A converter that panics on a crafted document never ends the process: the
// conversion runs on the copy goroutine, which net/http does not recover. The
// panic is the "could not be read" 400, no byte of the part reaches the pod,
// and the log names the panic's class only, never its value (it can carry
// document text).
func TestPutReferences_AConverterPanicIsA400(t *testing.T) {
	const secret = "document text from the upload"
	real := officeMarkdown
	officeMarkdown = func(string, []byte, int) (string, error) { panic(errors.New(secret)) }
	t.Cleanup(func() { officeMarkdown = real })
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	rec := &recordingRefs{}
	h := NewHandler(rec, memRepos(t, "default", "p", "https://github.com/acme/greeter"), nil)
	_, err := h.PutProjectReferences(tenantCtx("default"), multipartRequest(t, map[string][]byte{
		"policy.docx": wordDocument(t, "Receipts above $25."),
	}))
	var ae *apierr.Error
	if !errors.As(err, &ae) || ae.Status != http.StatusBadRequest {
		t.Fatalf("err = %v, want 400", err)
	}
	if want := `"policy.docx" could not be read as a .docx file`; ae.Message != want {
		t.Fatalf("message = %q, want %q", ae.Message, want)
	}
	if len(rec.parts) != 0 {
		t.Fatalf("the pod got %d parts, want none", len(rec.parts))
	}
	if !strings.Contains(logs.String(), `"msg":"references.office_conversion_panicked"`) ||
		!strings.Contains(logs.String(), `"class":"*errors.errorString"`) {
		t.Fatalf("log = %s, want the event with the panic's class", logs.String())
	}
	if strings.Contains(logs.String(), secret) {
		t.Fatalf("log carries the panic's value: %s", logs.String())
	}
}
