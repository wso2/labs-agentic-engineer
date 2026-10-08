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

// Package officetext turns a Word, Excel or PowerPoint file into markdown text
// a model can read (S5). The models read PDFs and images natively, and text as
// text, but not Office formats; a document the user attaches in one is
// converted once, on upload, so every turn reads its words.
//
// What survives is what a requirements reader cites: a Word document's
// headings and paragraphs (and its tables, as rows), an Excel workbook's
// sheets as tables, a PowerPoint deck's slides in order. Layout, images and
// formatting do not. Only the standard library: the formats are zipped XML.
//
// Output bounded by the caller's budget (changed from main #878 S5, to
// upstream): Markdown fails with ErrTooLarge as soon as the markdown would pass
// maxBytes, and every XML part it reads, re-reads included, counts toward one
// document-wide bound. A small zip can name one large part many times (deck
// entries, shared strings), so bounding each read alone does not bound the
// work or the text.
package officetext

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Extensions are the Office types Markdown converts.
var Extensions = map[string]bool{".docx": true, ".xlsx": true, ".pptx": true}

// ErrUnreadable is a file that is not the Office document its name says.
var ErrUnreadable = errors.New("officetext: not a readable Office document")

// ErrTooLarge is a document whose markdown would pass the caller's budget.
var ErrTooLarge = errors.New("officetext: the converted text is too large")

// maxXMLBytes bounds the XML read out of the zip, every part and every re-read
// of one together: a small file can inflate to anything, and a requirements
// document never needs more.
const maxXMLBytes = 32 << 20

// maxRows bounds a sheet: past it, the rest is summarised in a line.
const maxRows = 2000

// Markdown converts the document to markdown, by its extension (".docx"). The
// markdown is at most maxBytes long; a document that would pass it is
// ErrTooLarge, refused while it is converted.
func Markdown(ext string, content []byte, maxBytes int) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrUnreadable, err)
	}
	d := &document{parts: map[string]*zip.File{}, xmlLeft: maxXMLBytes}
	for _, f := range zr.File {
		d.parts[f.Name] = f
	}
	switch strings.ToLower(ext) {
	case ".docx":
		return d.word(&mdWriter{max: maxBytes, sep: "\n\n"})
	case ".xlsx":
		return d.excel(&mdWriter{max: maxBytes, sep: "\n"})
	case ".pptx":
		return d.powerPoint(&mdWriter{max: maxBytes, sep: "\n"})
	}
	return "", fmt.Errorf("%w: %s is not an Office type", ErrUnreadable, ext)
}

// document is an Office file's zip parts and what is left of its XML bound.
type document struct {
	parts   map[string]*zip.File
	xmlLeft int
}

// read reads one XML part, counting it toward the document's XML bound.
func (d *document) read(name string) ([]byte, error) {
	f, ok := d.parts[name]
	if !ok {
		return nil, fmt.Errorf("%w: no %s", ErrUnreadable, name)
	}
	rc, err := f.Open()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnreadable, err)
	}
	defer rc.Close()
	b, err := io.ReadAll(io.LimitReader(rc, int64(d.xmlLeft)+1))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnreadable, err)
	}
	if len(b) > d.xmlLeft {
		return nil, fmt.Errorf("%w: its XML passes %d MiB at %s", ErrUnreadable, maxXMLBytes>>20, name)
	}
	d.xmlLeft -= len(b)
	return b, nil
}

// mdWriter builds the markdown line by line, separated by sep, and refuses a
// line (ErrTooLarge) that would take it past max.
type mdWriter struct {
	b     strings.Builder
	max   int
	sep   string
	lines int
}

// fits reports whether a line of n bytes still fits.
func (w *mdWriter) fits(n int) bool {
	if w.lines > 0 {
		n += len(w.sep)
	}
	return w.b.Len()+n <= w.max
}

func (w *mdWriter) line(s string) error {
	if !w.fits(len(s)) {
		return ErrTooLarge
	}
	if w.lines > 0 {
		w.b.WriteString(w.sep)
	}
	w.b.WriteString(s)
	w.lines++
	return nil
}

// done appends trailer and returns the markdown.
func (w *mdWriter) done(trailer string) (string, error) {
	if w.b.Len()+len(trailer) > w.max {
		return "", ErrTooLarge
	}
	w.b.WriteString(trailer)
	return w.b.String(), nil
}

// tokens walks an XML part, calling fn for each start and end element and each
// run of text, by local name.
func tokens(b []byte, fn func(tok xml.Token)) error {
	d := xml.NewDecoder(bytes.NewReader(b))
	for {
		tok, err := d.Token()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("%w: %w", ErrUnreadable, err)
		}
		fn(tok)
	}
}

// relID is an element's relationship ID: the namespaced `r:id`, which sits
// beside a plain `id` on a slide's entry.
func relID(e xml.StartElement) string {
	for _, a := range e.Attr {
		if a.Name.Local == "id" && a.Name.Space != "" {
			return a.Value
		}
	}
	return ""
}

func attr(e xml.StartElement, local string) string {
	for _, a := range e.Attr {
		if a.Name.Local == local {
			return a.Value
		}
	}
	return ""
}

// ---- Word ------------------------------------------------------------------

var headingStyle = regexp.MustCompile(`(?i)^(?:heading|title)\s*([1-6])?$`)

// word reads word/document.xml: each paragraph a line, a Heading N paragraph a
// markdown heading (Title as the first level), a table one `|`-separated row
// per table row, each cell's paragraphs joined.
func (d *document) word(md *mdWriter) (string, error) {
	b, err := d.read("word/document.xml")
	if err != nil {
		return "", err
	}
	var out []string
	var para strings.Builder
	var row []*strings.Builder
	level, cells := 0, 0
	err = tokens(b, func(tok xml.Token) {
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "p":
				para.Reset()
				level = 0
			case "pStyle":
				if m := headingStyle.FindStringSubmatch(attr(t, "val")); m != nil {
					level = 1
					if m[1] != "" {
						level, _ = strconv.Atoi(m[1])
					}
				}
			case "tab":
				para.WriteString("\t")
			case "br", "cr":
				para.WriteString(" ")
			case "tr":
				row = nil
			case "tc":
				cells++
				row = append(row, &strings.Builder{})
			}
		case xml.CharData:
			para.Write(t)
		case xml.EndElement:
			switch t.Name.Local {
			case "p":
				text := strings.TrimSpace(para.String())
				para.Reset()
				switch {
				case text == "":
				case cells > 0 && len(row) > 0:
					cell := row[len(row)-1]
					if cell.Len() > 0 {
						cell.WriteString(" ")
					}
					cell.WriteString(text)
				case level > 0:
					out = append(out, strings.Repeat("#", level)+" "+text)
				default:
					out = append(out, text)
				}
			case "tc":
				cells--
			case "tr":
				if len(row) > 0 {
					texts := make([]string, len(row))
					for i, c := range row {
						texts[i] = c.String()
					}
					out = append(out, "| "+strings.Join(texts, " | ")+" |")
				}
				row = nil
			}
		}
	})
	if err != nil {
		return "", err
	}
	for _, l := range out {
		if err := md.line(l); err != nil {
			return "", err
		}
	}
	return md.done("\n")
}

// ---- Excel -----------------------------------------------------------------

// excel reads each sheet, in workbook order, as a markdown table under
// `## Sheet: <name>`; the first row is its header.
func (d *document) excel(md *mdWriter) (string, error) {
	shared, err := d.sharedStrings()
	if err != nil {
		return "", err
	}
	sheets, err := d.workbookSheets()
	if err != nil {
		return "", err
	}
	for _, s := range sheets {
		rows, err := d.sheetRows(s.target, shared)
		if err != nil {
			return "", err
		}
		if err := md.line("## Sheet: " + s.name); err != nil {
			return "", err
		}
		if err := table(rows, md); err != nil {
			return "", err
		}
	}
	return md.done("\n")
}

func (d *document) sharedStrings() ([]string, error) {
	if _, ok := d.parts["xl/sharedStrings.xml"]; !ok {
		return nil, nil
	}
	b, err := d.read("xl/sharedStrings.xml")
	if err != nil {
		return nil, err
	}
	var out []string
	var cur strings.Builder
	inT := false
	err = tokens(b, func(tok xml.Token) {
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "si" {
				cur.Reset()
			}
			inT = t.Name.Local == "t"
		case xml.CharData:
			if inT {
				cur.Write(t)
			}
		case xml.EndElement:
			if t.Name.Local == "t" {
				inT = false
			}
			if t.Name.Local == "si" {
				out = append(out, cur.String())
			}
		}
	})
	return out, err
}

type sheetRef struct{ name, target string }

// workbookSheets lists the sheets in workbook order with their part paths.
func (d *document) workbookSheets() ([]sheetRef, error) {
	rels, err := d.relationships("xl/_rels/workbook.xml.rels", "xl")
	if err != nil {
		return nil, err
	}
	b, err := d.read("xl/workbook.xml")
	if err != nil {
		return nil, err
	}
	var out []sheetRef
	err = tokens(b, func(tok xml.Token) {
		if t, ok := tok.(xml.StartElement); ok && t.Name.Local == "sheet" {
			if target := rels[relID(t)]; target != "" {
				out = append(out, sheetRef{name: attr(t, "name"), target: target})
			}
		}
	})
	return out, err
}

// relationships maps a part's relationship IDs to the part paths they name.
func (d *document) relationships(name, base string) (map[string]string, error) {
	b, err := d.read(name)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	err = tokens(b, func(tok xml.Token) {
		if t, ok := tok.(xml.StartElement); ok && t.Name.Local == "Relationship" {
			target := attr(t, "Target")
			if !strings.HasPrefix(target, "/") {
				target = path.Join(base, target)
			}
			out[attr(t, "Id")] = strings.TrimPrefix(target, "/")
		}
	})
	return out, err
}

var cellColumn = regexp.MustCompile(`^([A-Z]+)`)

func columnIndex(ref string) int {
	m := cellColumn.FindString(ref)
	n := 0
	for _, c := range m {
		n = n*26 + int(c-'A'+1)
	}
	return n - 1
}

// sheetRows is a sheet's text: the first maxRows rows that are not blank, as
// cells, its widest row, and how many more rows have text.
type sheetRows struct {
	rows  [][]string
	width int
	more  int
}

// sheetCell is one cell's text at its column.
type sheetCell struct {
	col  int
	text string
}

// maxColumns bounds a row: cells past it are dropped.
const maxColumns = 1000

// sheetRows reads a sheet's cells into rows of text. A row's cells are held
// sparsely and padded only when the row is kept: blank rows are dropped and
// rows past maxRows only counted, since every kept row pads to its widest
// cell. A cell whose reference names no column (or a column past maxColumns)
// is dropped.
func (d *document) sheetRows(name string, shared []string) (sheetRows, error) {
	var out sheetRows
	b, err := d.read(name)
	if err != nil {
		return out, err
	}
	var cells []sheetCell
	rowLen, blank := 0, true
	var cellType, cellRef string
	var val strings.Builder
	inV := false
	err = tokens(b, func(tok xml.Token) {
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "row":
				cells, rowLen, blank = nil, 0, true
			case "c":
				cellType, cellRef = attr(t, "t"), attr(t, "r")
				val.Reset()
			case "v", "t":
				inV = true
			}
		case xml.CharData:
			if inV {
				val.Write(t)
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "v", "t":
				inV = false
			case "c":
				text := val.String()
				if cellType == "s" {
					if i, err := strconv.Atoi(text); err == nil && i >= 0 && i < len(shared) {
						text = shared[i]
					}
				}
				col := rowLen
				if cellRef != "" {
					col = columnIndex(cellRef)
				}
				if col < 0 || col >= maxColumns {
					break
				}
				rowLen = max(rowLen, col+1)
				text = strings.TrimSpace(text)
				if text != "" {
					blank = false
					cells = append(cells, sheetCell{col: col, text: text})
				}
			case "row":
				if blank {
					break
				}
				out.width = max(out.width, rowLen)
				if len(out.rows) >= maxRows {
					out.more++
					break
				}
				row := make([]string, rowLen)
				for _, c := range cells {
					row[c.col] = c.text
				}
				out.rows = append(out.rows, row)
			}
		}
	})
	return out, err
}

// table writes a sheet as a markdown table, its first row the header, each
// line checked against the budget before it is built.
func table(s sheetRows, md *mdWriter) error {
	if len(s.rows) == 0 {
		return writeLines(md, "", "(empty)", "")
	}
	line := func(r []string) (string, error) {
		n := len("| ") + len(" |") + len(" | ")*(s.width-1)
		for _, c := range r {
			n += len(c) + strings.Count(c, "|")
		}
		if !md.fits(n) {
			return "", ErrTooLarge
		}
		cells := make([]string, s.width)
		for i := range cells {
			if i < len(r) {
				cells[i] = strings.ReplaceAll(r[i], "|", "\\|")
			}
		}
		return "| " + strings.Join(cells, " | ") + " |", nil
	}
	header, err := line(s.rows[0])
	if err != nil {
		return err
	}
	if err := writeLines(md, "", header, "|"+strings.Repeat(" --- |", s.width)); err != nil {
		return err
	}
	for _, r := range s.rows[1:] {
		l, err := line(r)
		if err != nil {
			return err
		}
		if err := md.line(l); err != nil {
			return err
		}
	}
	if s.more > 0 {
		if err := writeLines(md, "", fmt.Sprintf("(%d more rows not shown)", s.more)); err != nil {
			return err
		}
	}
	return md.line("")
}

func writeLines(md *mdWriter, lines ...string) error {
	for _, l := range lines {
		if err := md.line(l); err != nil {
			return err
		}
	}
	return nil
}

// ---- PowerPoint ------------------------------------------------------------

var slideName = regexp.MustCompile(`^ppt/slides/slide(\d+)\.xml$`)

// powerPoint reads each slide's text, in deck order, under `## Slide N`.
func (d *document) powerPoint(md *mdWriter) (string, error) {
	order, err := d.slideOrder()
	if err != nil {
		return "", err
	}
	for i, name := range order {
		b, err := d.read(name)
		if err != nil {
			return "", err
		}
		var lines []string
		var para strings.Builder
		err = tokens(b, func(tok xml.Token) {
			switch t := tok.(type) {
			case xml.StartElement:
				if t.Name.Local == "p" {
					para.Reset()
				}
			case xml.CharData:
				para.Write(t)
			case xml.EndElement:
				if t.Name.Local == "p" {
					if text := strings.TrimSpace(para.String()); text != "" {
						lines = append(lines, text)
					}
				}
			}
		})
		if err != nil {
			return "", err
		}
		if err := writeLines(md, fmt.Sprintf("## Slide %d", i+1), ""); err != nil {
			return "", err
		}
		for _, l := range lines {
			if err := md.line("- " + l); err != nil {
				return "", err
			}
		}
		if err := md.line(""); err != nil {
			return "", err
		}
	}
	return md.done("")
}

// slideOrder is the deck's order (presentation.xml), else the slides by number.
func (d *document) slideOrder() ([]string, error) {
	if _, ok := d.parts["ppt/presentation.xml"]; ok {
		if rels, err := d.relationships("ppt/_rels/presentation.xml.rels", "ppt"); err == nil {
			b, err := d.read("ppt/presentation.xml")
			if err != nil {
				return nil, err
			}
			var out []string
			err = tokens(b, func(tok xml.Token) {
				if t, ok := tok.(xml.StartElement); ok && t.Name.Local == "sldId" {
					if target := rels[relID(t)]; target != "" {
						out = append(out, target)
					}
				}
			})
			if err == nil && len(out) > 0 {
				return out, nil
			}
		}
	}
	var out []string
	for name := range d.parts {
		if slideName.MatchString(name) {
			out = append(out, name)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, _ := strconv.Atoi(slideName.FindStringSubmatch(out[i])[1])
		b, _ := strconv.Atoi(slideName.FindStringSubmatch(out[j])[1])
		return a < b
	})
	return out, nil
}
