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

package specbundle

import (
	"archive/zip"
	"bytes"
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"
)

const manifest = `{
  "format": "aespecs/1",
  "producer": { "name": "code-to-spec", "version": "1.0" },
  "product": "Allocations",
  "source": { "repository": "https://github.com/acme/allocations.git", "commit": "d14b825" },
  "createdAt": "2026-10-21T08:00:00Z"
}`

// entry is one archive member; mode lets a test plant a symlink.
type entry struct {
	name    string
	content string
	mode    os.FileMode
}

func zipOf(t *testing.T, entries []entry) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, e := range entries {
		h := &zip.FileHeader{Name: e.name, Method: zip.Deflate}
		if e.mode != 0 {
			h.SetMode(e.mode)
		}
		f, err := w.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write([]byte(e.content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func valid() []entry {
	return []entry{
		{name: ManifestPath, content: manifest},
		{name: "specs/", content: ""},
		{name: "specs/requirements/prd.md", content: "# Allocations\n\n## Features\n"},
		{name: "specs/requirements/features/F1-engagements.md", content: "# F1 Engagements\n"},
		{name: "specs/requirements/sources/backend.md", content: "# backend\n"},
		{name: "specs/design/domain-model.md", content: "# Data\n\n```mermaid\nerDiagram\n```\n"},
		{name: "specs/design/dependencies/sales-entity-service/dependency.json", content: `{"name":"sales-entity-service","resource":{"name":"sales-entity-service"}}`},
		{name: "specs/design/dependencies/sales-entity-service/schema.graphql", content: "type Query { customers: [String!]! }\n"},
		{name: "specs/design/dependencies/sales-entity-service/assumptions.md", content: "# What the design assumes\n"},
	}
}

func withEntry(e entry) []entry { return append(valid(), e) }

func TestOpen_ValidBundle(t *testing.T) {
	b, err := Open(zipOf(t, valid()))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if b.Manifest.Product != "Allocations" || b.Manifest.Source.Commit != "d14b825" || b.Manifest.Producer.Name != "code-to-spec" {
		t.Errorf("manifest = %+v", b.Manifest)
	}
	if _, ok := b.Files[ManifestPath]; ok {
		t.Error("the manifest must not be among the files")
	}
	want := []string{
		"specs/requirements/prd.md",
		"specs/requirements/features/F1-engagements.md",
		"specs/requirements/sources/backend.md",
		"specs/design/domain-model.md",
		"specs/design/dependencies/sales-entity-service/dependency.json",
		"specs/design/dependencies/sales-entity-service/schema.graphql",
		"specs/design/dependencies/sales-entity-service/assumptions.md",
	}
	if len(b.Files) != len(want) {
		t.Fatalf("files = %d, want %d", len(b.Files), len(want))
	}
	for _, p := range want {
		if _, ok := b.Files[p]; !ok {
			t.Errorf("missing %s", p)
		}
	}
}

func TestOpen_Refusals(t *testing.T) {
	cases := map[string]struct {
		entries []entry
		want    string
	}{
		"not a zip":             {entries: nil, want: "not a zip archive"},
		"no manifest":           {entries: valid()[1:], want: "no manifest.json"},
		"manifest only":         {entries: valid()[:1], want: "no spec files"},
		"traversal":             {entries: withEntry(entry{name: "specs/requirements/../../etc/passwd.md", content: "x"}), want: "not a plain relative path"},
		"absolute":              {entries: withEntry(entry{name: "/specs/requirements/x.md", content: "x"}), want: "not a plain relative path"},
		"dot segment":           {entries: withEntry(entry{name: "specs/requirements/.hidden.md", content: "x"}), want: "dot-led segment"},
		"descriptor":            {entries: withEntry(entry{name: "specs/.agentic-engineer.toml", content: "x"}), want: "dot-led segment"},
		"references":            {entries: withEntry(entry{name: "specs/requirements/references/brief.md", content: "x"}), want: "reference documents"},
		"outside specs":         {entries: withEntry(entry{name: "src/main.go", content: "package main"}), want: "outside the spec tree"},
		"design file":           {entries: withEntry(entry{name: "specs/design/design.cell", content: "title x"}), want: "outside the spec tree"},
		"requirements non-md":   {entries: withEntry(entry{name: "specs/requirements/notes.txt", content: "x"}), want: "markdown"},
		"dependency stray file": {entries: withEntry(entry{name: "specs/design/dependencies/sales-entity-service/notes.yaml", content: "x: 1"}), want: "not a dependency definition"},
		"dependency nested":     {entries: withEntry(entry{name: "specs/design/dependencies/sales-entity-service/nested/openapi.yaml", content: "x"}), want: "directly in its definition's directory"},
		"dependency not kebab":  {entries: withEntry(entry{name: "specs/design/dependencies/Sales_Entity/dependency.json", content: "{}"}), want: "kebab-case"},
		"symlink":               {entries: withEntry(entry{name: "specs/requirements/link.md", content: "../x", mode: os.ModeSymlink | 0o777}), want: "symbolic link"},
		"nul byte":              {entries: withEntry(entry{name: "specs/requirements/bin.md", content: "a\x00b"}), want: "not UTF-8 text"},
		"invalid utf-8":         {entries: withEntry(entry{name: "specs/requirements/bad.md", content: "\xff\xfe"}), want: "not UTF-8 text"},
		"private key":           {entries: withEntry(entry{name: "specs/requirements/key.md", content: "-----BEGIN RSA PRIVATE KEY-----\nabc\n"}), want: "private key block"},
		"assigned secret":       {entries: withEntry(entry{name: "specs/requirements/cfg.md", content: "client_secret: 8f3a9c2e1b7d4e6f0a1b2c3d\n"}), want: `assigned to "client_secret"`},
		"assigned passphrase":   {entries: withEntry(entry{name: "specs/requirements/cfg.md", content: "password = correcthorsebatterystaple\n"}), want: `assigned to "password"`},
		"url credentials":       {entries: withEntry(entry{name: "specs/requirements/url.md", content: "see https://user:pa55word@example.com/x\n"}), want: "URL with credentials"},
		"high entropy token":    {entries: withEntry(entry{name: "specs/requirements/tok.md", content: "ref AKIAxQ7bL2mN9pR4sT6vW8yZ1aC3eF5gH0jK2lM4nP6\n"}), want: "high-entropy token"},
		"prefixed token":        {entries: withEntry(entry{name: "specs/requirements/tok.md", content: "ref sk-AbC9dEf2GhI5jKl8MnO1pQr4StU7vWx0YzA3bCd6\n"}), want: "high-entropy token"},
		"url-safe token":        {entries: withEntry(entry{name: "specs/requirements/tok.md", content: "ref aZ9x-Qm3L_tR8vK2nP5wY7cH1jD4fG6sB0eN3uI9oA\n"}), want: "high-entropy token"},
		"two-word prefix token": {entries: withEntry(entry{name: "specs/requirements/tok.md", content: "ref sk-proj-AbC9dEf2GhI5jKl8MnO1pQr4StU7vWx0YzA3bCd6\n"}), want: "high-entropy token"},
		"underscored key":       {entries: withEntry(entry{name: "specs/requirements/tok.md", content: "ref svc_key_Qm3LtR8vK2nP5wY7cH1jD4fG6sB0eN3u\n"}), want: "high-entropy token"},
		"token under a path":    {entries: withEntry(entry{name: "specs/requirements/tok.md", content: "see com/api/keys/aZ9xQm3LtR8vK2nP5wY7cH1jD4fG6sB0eN3uI9oA\n"}), want: "high-entropy token"},
		"single-letter words":   {entries: withEntry(entry{name: "specs/requirements/tok.md", content: "ref x-a-Qm3LtR8vK2nP5wY7cH1jD4fG6sB0eN3uI9oAbCd\n"}), want: "high-entropy token"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var data []byte
			if tc.entries == nil {
				data = []byte("not a zip")
			} else {
				data = zipOf(t, tc.entries)
			}
			_, err := Open(data)
			if !errors.Is(err, ErrRejected) {
				t.Fatalf("err = %v, want ErrRejected", err)
			}
			if !strings.Contains(Reason(err), tc.want) {
				t.Errorf("reason = %q, want it to contain %q", Reason(err), tc.want)
			}
		})
	}
}

func TestOpen_ProseIsNotACredential(t *testing.T) {
	prose := "**Consumption instructions**: send the bearer token: authenticate first, then call `GET /rates`.\n" +
		"| SALES_ENTITY_SERVICE_CLIENT_SECRET | Client secret for the client-credentials grant | yes |\n" +
		"Read from https://github.com/acme/allocations.git at d14b825c0ffee0000000000000000000000000000.\n" +
		`{ "config": [ { "key": "SALES_ENTITY_SERVICE_CLIENT_SECRET", "secret": true } ] }` + "\n" +
		"- F12 [Visas and travel documents](features/F12-visas-and-travel-documents.md)\n" +
		"- travel/backend-go/internal/handlers/TravelRequestHandler2.go:88 → F11.3\n" +
		"- travel/backend-go/internal/api/v1/handlers/TravelRequestHandler.go:40 → F11.2\n" +
		"- see also [F13](features/F12-visas-and-travel-documents-F13.md)\n"
	if _, err := Open(zipOf(t, withEntry(entry{name: "specs/requirements/prose.md", content: prose}))); err != nil {
		t.Fatalf("prose refused: %v", err)
	}
}

func TestOpen_Bounds(t *testing.T) {
	t.Run("too many entries", func(t *testing.T) {
		entries := valid()
		for i := 0; len(entries) <= MaxEntries; i++ {
			entries = append(entries, entry{name: "specs/requirements/sources/m" + strings.Repeat("x", i%3) + string(rune('a'+i%26)) + strconv.Itoa(i) + ".md", content: "# m\n"})
		}
		_, err := Open(zipOf(t, entries))
		if !errors.Is(err, ErrRejected) || !strings.Contains(Reason(err), "entries") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("file over the per-file cap", func(t *testing.T) {
		big := strings.Repeat("a", MaxFileBytes+1)
		_, err := Open(zipOf(t, withEntry(entry{name: "specs/requirements/big.md", content: big})))
		if !errors.Is(err, ErrRejected) || !strings.Contains(Reason(err), "over 5 MiB") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("files over the unpacked cap", func(t *testing.T) {
		half := strings.Repeat("b", 4<<20+1)
		entries := withEntry(entry{name: "specs/requirements/one.md", content: half})
		entries = append(entries, entry{name: "specs/requirements/two.md", content: half})
		_, err := Open(zipOf(t, entries))
		if !errors.Is(err, ErrRejected) || !strings.Contains(Reason(err), "unpack to more than") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("bundle over the archive cap", func(t *testing.T) {
		_, err := Open(make([]byte, MaxBundleBytes+1))
		if !errors.Is(err, ErrRejected) || !strings.Contains(Reason(err), "bundle is over") {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestParseManifest_Refusals(t *testing.T) {
	cases := map[string]string{
		"unknown field":  `{"format":"aespecs/1","producer":{"name":"x","version":"1"},"product":"p","source":{"repository":"local checkout","commit":"abcdef1"},"createdAt":"2026-10-21T08:00:00Z","extra":1}`,
		"other format":   `{"format":"aespecs/2","producer":{"name":"x","version":"1"},"product":"p","source":{"repository":"local checkout","commit":"abcdef1"},"createdAt":"2026-10-21T08:00:00Z"}`,
		"bad producer":   `{"format":"aespecs/1","producer":{"name":"Code To Spec","version":"1"},"product":"p","source":{"repository":"local checkout","commit":"abcdef1"},"createdAt":"2026-10-21T08:00:00Z"}`,
		"bad repository": `{"format":"aespecs/1","producer":{"name":"x","version":"1"},"product":"p","source":{"repository":"ftp://x","commit":"abcdef1"},"createdAt":"2026-10-21T08:00:00Z"}`,
		"bad commit":     `{"format":"aespecs/1","producer":{"name":"x","version":"1"},"product":"p","source":{"repository":"local checkout","commit":"main"},"createdAt":"2026-10-21T08:00:00Z"}`,
		"bad createdAt":  `{"format":"aespecs/1","producer":{"name":"x","version":"1"},"product":"p","source":{"repository":"local checkout","commit":"abcdef1"},"createdAt":"yesterday"}`,
		"empty product":  `{"format":"aespecs/1","producer":{"name":"x","version":"1"},"product":" ","source":{"repository":"local checkout","commit":"abcdef1"},"createdAt":"2026-10-21T08:00:00Z"}`,
		"not json":       `{`,
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := parseManifest([]byte(raw)); !errors.Is(err, ErrRejected) {
				t.Fatalf("err = %v, want ErrRejected", err)
			}
		})
	}
	if m, err := parseManifest([]byte(strings.Replace(manifest, "https://github.com/acme/allocations.git", LocalCheckout, 1))); err != nil || m.Source.Repository != LocalCheckout {
		t.Fatalf("local checkout refused: %v", err)
	}
}
