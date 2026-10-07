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

package officetext

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"testing"
)

// unbounded is an output budget no fixture here reaches.
const unbounded = 1 << 30

// zipOf builds an Office file from its parts, as Word, Excel and PowerPoint
// write them (only the parts and elements the reader uses).
func zipOf(t *testing.T, parts map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, content := range parts {
		f, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

const w = `xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"`

func TestWord(t *testing.T) {
	doc := zipOf(t, map[string]string{"word/document.xml": `<w:document ` + w + `><w:body>
<w:p><w:pPr><w:pStyle w:val="Heading1"/></w:pPr><w:r><w:t>Expenses policy</w:t></w:r></w:p>
<w:p><w:r><w:t>Receipts are required </w:t></w:r><w:r><w:t>above $25.</w:t></w:r></w:p>
<w:p><w:pPr><w:pStyle w:val="Heading2"/></w:pPr><w:r><w:t>Limits</w:t></w:r></w:p>
<w:tbl><w:tr><w:tc><w:p><w:r><w:t>Meals</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>$50</w:t></w:r></w:p></w:tc></w:tr></w:tbl>
</w:body></w:document>`})
	got, err := Markdown(".docx", doc, unbounded)
	if err != nil {
		t.Fatal(err)
	}
	want := "# Expenses policy\n\nReceipts are required above $25.\n\n## Limits\n\n| Meals | $50 |\n"
	if got != want {
		t.Errorf("got\n%q\nwant\n%q", got, want)
	}
}

func TestExcel(t *testing.T) {
	doc := zipOf(t, map[string]string{
		"xl/workbook.xml":            `<workbook xmlns:r="r"><sheets><sheet name="Rates" sheetId="1" r:id="rId1"/></sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": `<Relationships><Relationship Id="rId1" Target="worksheets/sheet1.xml"/></Relationships>`,
		"xl/sharedStrings.xml":       `<sst><si><t>Country</t></si><si><t>Per diem</t></si><si><t>LK</t></si></sst>`,
		"xl/worksheets/sheet1.xml": `<worksheet><sheetData>
<row r="1"><c r="A1" t="s"><v>0</v></c><c r="B1" t="s"><v>1</v></c></row>
<row r="2"><c r="A2" t="s"><v>2</v></c><c r="B2"><v>40</v></c></row>
</sheetData></worksheet>`,
	})
	got, err := Markdown(".xlsx", doc, unbounded)
	if err != nil {
		t.Fatal(err)
	}
	want := "## Sheet: Rates\n\n| Country | Per diem |\n| --- | --- |\n| LK | 40 |\n\n"
	if got != want {
		t.Errorf("got\n%q\nwant\n%q", got, want)
	}
}

func TestPowerPoint(t *testing.T) {
	doc := zipOf(t, map[string]string{
		"ppt/presentation.xml":            `<p:presentation xmlns:p="p" xmlns:r="r"><p:sldIdLst><p:sldId id="257" r:id="rId3"/><p:sldId id="256" r:id="rId2"/></p:sldIdLst></p:presentation>`,
		"ppt/_rels/presentation.xml.rels": `<Relationships><Relationship Id="rId2" Target="slides/slide1.xml"/><Relationship Id="rId3" Target="slides/slide2.xml"/></Relationships>`,
		"ppt/slides/slide1.xml":           `<p:sld xmlns:p="p" xmlns:a="a"><a:p><a:r><a:t>Second</a:t></a:r></a:p></p:sld>`,
		"ppt/slides/slide2.xml":           `<p:sld xmlns:p="p" xmlns:a="a"><a:p><a:r><a:t>Approvals </a:t></a:r><a:r><a:t>over $1,000</a:t></a:r></a:p></p:sld>`,
	})
	got, err := Markdown(".pptx", doc, unbounded)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, "## Slide 1\n\n- Approvals over $1,000\n\n## Slide 2\n\n- Second") {
		t.Errorf("got\n%s", got)
	}
}

func TestNotAnOfficeFile(t *testing.T) {
	if _, err := Markdown(".docx", []byte("%PDF-1.4"), unbounded); err == nil {
		t.Error("a PDF named .docx was read")
	}
}

// The output stops at the caller's budget: one byte past it is ErrTooLarge,
// an exact fit converts.
func TestMarkdown_OutputPastTheBudgetIsErrTooLarge(t *testing.T) {
	doc := zipOf(t, map[string]string{"word/document.xml": `<w:document ` + w + `><w:body>
<w:p><w:r><w:t>Receipts above $25.</w:t></w:r></w:p></w:body></w:document>`})
	want := "Receipts above $25.\n"
	if got, err := Markdown(".docx", doc, len(want)); err != nil || got != want {
		t.Fatalf("exact fit = (%q, %v), want (%q, nil)", got, err, want)
	}
	if _, err := Markdown(".docx", doc, len(want)-1); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("one byte past = %v, want ErrTooLarge", err)
	}
}

// Every XML part read counts toward one document-wide bound, so deck entries
// that all name one large slide cannot make the reader inflate and parse it
// over and over.
func TestPowerPoint_RepeatedSlideReadsShareOneBound(t *testing.T) {
	deck := strings.Repeat(`<p:sldId r:id="rId1"/>`, 5)
	doc := zipOf(t, map[string]string{
		"ppt/presentation.xml":            `<p:presentation xmlns:p="p" xmlns:r="r"><p:sldIdLst>` + deck + `</p:sldIdLst></p:presentation>`,
		"ppt/_rels/presentation.xml.rels": `<Relationships><Relationship Id="rId1" Target="slides/slide1.xml"/></Relationships>`,
		"ppt/slides/slide1.xml":           `<p:sld xmlns:p="p">` + strings.Repeat("<x/>", (8<<20)/4) + `</p:sld>`,
	})
	if _, err := Markdown(".pptx", doc, unbounded); !errors.Is(err, ErrUnreadable) {
		t.Fatalf("five reads of an 8 MiB slide = %v, want ErrUnreadable (past the document's XML bound)", err)
	}
}

// A sheet keeps only the rows it shows: blank rows and rows past maxRows are
// counted, not held, so many short rows that each pad to a wide one cannot
// grow memory with the row count.
func TestExcel_RowsPastTheShownOnesAreNotHeld(t *testing.T) {
	var sheet strings.Builder
	const rows = 20000
	for r := 1; r <= rows; r++ {
		fmt.Fprintf(&sheet, `<row><c r="ALL%d" t="inlineStr"><is><t>x</t></is></c></row><row/>`, r)
	}
	doc := zipOf(t, map[string]string{
		"xl/workbook.xml":            `<workbook xmlns:r="r"><sheets><sheet name="S" sheetId="1" r:id="rId1"/></sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": `<Relationships><Relationship Id="rId1" Target="worksheets/sheet1.xml"/></Relationships>`,
		"xl/worksheets/sheet1.xml":   `<worksheet><sheetData>` + sheet.String() + `</sheetData></worksheet>`,
	})
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	got, err := Markdown(".xlsx", doc, unbounded)
	runtime.ReadMemStats(&after)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, fmt.Sprintf("(%d more rows not shown)", rows-maxRows)) {
		t.Fatalf("missing the not-shown count: %q", got[len(got)-80:])
	}
	if alloc := after.TotalAlloc - before.TotalAlloc; alloc > 150<<20 {
		t.Fatalf("allocated %d MiB, want rows past the shown ones not held", alloc>>20)
	}
}

// A cell whose reference names no column is dropped, not indexed at -1 (the
// conversion runs on the upload's goroutine, where a panic ends the process).
func TestExcel_ACellWithNoColumnIsDropped(t *testing.T) {
	doc := zipOf(t, map[string]string{
		"xl/workbook.xml":            `<workbook xmlns:r="r"><sheets><sheet name="S" sheetId="1" r:id="rId1"/></sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": `<Relationships><Relationship Id="rId1" Target="worksheets/sheet1.xml"/></Relationships>`,
		"xl/worksheets/sheet1.xml": `<worksheet><sheetData><row>` +
			`<c r="A1" t="inlineStr"><is><t>kept</t></is></c><c r="1" t="inlineStr"><is><t>dropped</t></is></c>` +
			`</row></sheetData></worksheet>`,
	})
	got, err := Markdown(".xlsx", doc, unbounded)
	if err != nil {
		t.Fatal(err)
	}
	if want := "## Sheet: S\n\n| kept |\n| --- |\n\n"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
