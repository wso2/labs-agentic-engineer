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

// maxPartBytes bounds one XML part as read out of the zip: a small file can
// inflate to anything, and a requirements document never needs more.
const maxPartBytes = 32 << 20

// maxRows bounds a sheet: past it, the rest is summarised in a line.
const maxRows = 2000

// Markdown converts the document to markdown, by its extension (".docx").
func Markdown(ext string, content []byte) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrUnreadable, err)
	}
	parts := map[string]*zip.File{}
	for _, f := range zr.File {
		parts[f.Name] = f
	}
	switch strings.ToLower(ext) {
	case ".docx":
		return word(parts)
	case ".xlsx":
		return excel(parts)
	case ".pptx":
		return powerPoint(parts)
	}
	return "", fmt.Errorf("%w: %s is not an Office type", ErrUnreadable, ext)
}

func read(parts map[string]*zip.File, name string) ([]byte, error) {
	f, ok := parts[name]
	if !ok {
		return nil, fmt.Errorf("%w: no %s", ErrUnreadable, name)
	}
	rc, err := f.Open()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnreadable, err)
	}
	defer rc.Close()
	b, err := io.ReadAll(io.LimitReader(rc, maxPartBytes+1))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnreadable, err)
	}
	if len(b) > maxPartBytes {
		return nil, fmt.Errorf("%w: %s is too large", ErrUnreadable, name)
	}
	return b, nil
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
func word(parts map[string]*zip.File) (string, error) {
	b, err := read(parts, "word/document.xml")
	if err != nil {
		return "", err
	}
	var out []string
	var para strings.Builder
	var row []string
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
				row = append(row, "")
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
					if row[len(row)-1] == "" {
						row[len(row)-1] = text
					} else {
						row[len(row)-1] += " " + text
					}
				case level > 0:
					out = append(out, strings.Repeat("#", level)+" "+text)
				default:
					out = append(out, text)
				}
			case "tc":
				cells--
			case "tr":
				if len(row) > 0 {
					out = append(out, "| "+strings.Join(row, " | ")+" |")
				}
				row = nil
			}
		}
	})
	if err != nil {
		return "", err
	}
	return strings.Join(out, "\n\n") + "\n", nil
}

// ---- Excel -----------------------------------------------------------------

// excel reads each sheet, in workbook order, as a markdown table under
// `## Sheet: <name>`; the first row is its header.
func excel(parts map[string]*zip.File) (string, error) {
	shared, err := sharedStrings(parts)
	if err != nil {
		return "", err
	}
	sheets, err := workbookSheets(parts)
	if err != nil {
		return "", err
	}
	var out []string
	for _, s := range sheets {
		rows, err := sheetRows(parts, s.target, shared)
		if err != nil {
			return "", err
		}
		out = append(out, "## Sheet: "+s.name)
		out = append(out, table(rows)...)
	}
	return strings.Join(out, "\n") + "\n", nil
}

func sharedStrings(parts map[string]*zip.File) ([]string, error) {
	if _, ok := parts["xl/sharedStrings.xml"]; !ok {
		return nil, nil
	}
	b, err := read(parts, "xl/sharedStrings.xml")
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
func workbookSheets(parts map[string]*zip.File) ([]sheetRef, error) {
	rels, err := relationships(parts, "xl/_rels/workbook.xml.rels", "xl")
	if err != nil {
		return nil, err
	}
	b, err := read(parts, "xl/workbook.xml")
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
func relationships(parts map[string]*zip.File, name, base string) (map[string]string, error) {
	b, err := read(parts, name)
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

// sheetRows reads a sheet's cells into rows of text.
func sheetRows(parts map[string]*zip.File, name string, shared []string) ([][]string, error) {
	b, err := read(parts, name)
	if err != nil {
		return nil, err
	}
	var rows [][]string
	var row []string
	var cellType, cellRef string
	var val strings.Builder
	inV := false
	err = tokens(b, func(tok xml.Token) {
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "row":
				row = nil
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
				col := len(row)
				if cellRef != "" {
					col = columnIndex(cellRef)
				}
				for len(row) <= col && col < 1000 {
					row = append(row, "")
				}
				if col < len(row) {
					row[col] = strings.TrimSpace(text)
				}
			case "row":
				rows = append(rows, row)
			}
		}
	})
	return rows, err
}

func table(rows [][]string) []string {
	var keep [][]string
	width := 0
	for _, r := range rows {
		if strings.TrimSpace(strings.Join(r, "")) == "" {
			continue
		}
		keep = append(keep, r)
		width = max(width, len(r))
	}
	if len(keep) == 0 {
		return []string{"", "(empty)", ""}
	}
	cut := 0
	if len(keep) > maxRows {
		cut = len(keep) - maxRows
		keep = keep[:maxRows]
	}
	line := func(r []string) string {
		cells := make([]string, width)
		for i := range cells {
			if i < len(r) {
				cells[i] = strings.ReplaceAll(r[i], "|", "\\|")
			}
		}
		return "| " + strings.Join(cells, " | ") + " |"
	}
	out := []string{"", line(keep[0]), "|" + strings.Repeat(" --- |", width)}
	for _, r := range keep[1:] {
		out = append(out, line(r))
	}
	if cut > 0 {
		out = append(out, "", fmt.Sprintf("(%d more rows not shown)", cut))
	}
	return append(out, "")
}

// ---- PowerPoint ------------------------------------------------------------

var slideName = regexp.MustCompile(`^ppt/slides/slide(\d+)\.xml$`)

// powerPoint reads each slide's text, in deck order, under `## Slide N`.
func powerPoint(parts map[string]*zip.File) (string, error) {
	order, err := slideOrder(parts)
	if err != nil {
		return "", err
	}
	var out []string
	for i, name := range order {
		b, err := read(parts, name)
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
		out = append(out, fmt.Sprintf("## Slide %d", i+1), "")
		for _, l := range lines {
			out = append(out, "- "+l)
		}
		out = append(out, "")
	}
	return strings.Join(out, "\n"), nil
}

// slideOrder is the deck's order (presentation.xml), else the slides by number.
func slideOrder(parts map[string]*zip.File) ([]string, error) {
	if _, ok := parts["ppt/presentation.xml"]; ok {
		if rels, err := relationships(parts, "ppt/_rels/presentation.xml.rels", "ppt"); err == nil {
			b, err := read(parts, "ppt/presentation.xml")
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
	for name := range parts {
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
