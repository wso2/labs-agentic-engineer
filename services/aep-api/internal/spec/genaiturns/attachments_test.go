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

package genaiturns

import (
	"bytes"
	"encoding/base64"
	"mime/multipart"
	"strings"
	"testing"
)

// buildMultipart writes a create-turn multipart body and returns a reader over
// it, mirroring what the console's FormData produces.
func buildMultipart(t *testing.T, fields map[string]string, files [][2]any) *multipart.Reader {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for k, v := range fields {
		if err := w.WriteField(k, v); err != nil {
			t.Fatalf("write field %q: %v", k, err)
		}
	}
	for _, f := range files {
		name := f[0].(string)
		part, err := w.CreateFormFile(attachmentsField, name)
		if err != nil {
			t.Fatalf("create file part %q: %v", name, err)
		}
		switch body := f[1].(type) {
		case string:
			_, _ = part.Write([]byte(body))
		case int:
			_, _ = part.Write(make([]byte, body))
		default:
			t.Fatalf("unsupported body for %q", name)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	return multipart.NewReader(&buf, w.Boundary())
}

func TestReadMultipartTurnFields(t *testing.T) {
	r := buildMultipart(t, map[string]string{
		"instruction": "what is wrong here?",
		"collab":      "true",
		"target":      "specs/requirements/requirements.md",
	}, [][2]any{{"shot.png", "bytes"}})

	got, err := readMultipartTurn(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Instruction != "what is wrong here?" {
		t.Errorf("instruction = %q", got.Instruction)
	}
	if !got.Collab {
		t.Error("collab should be true — a form field has no boolean type, so \"true\" must parse")
	}
	if got.Target != "specs/requirements/requirements.md" {
		t.Errorf("target = %q", got.Target)
	}
	if len(got.Attachments) != 1 || got.Attachments[0].Name != "shot.png" {
		t.Fatalf("attachments = %+v", got.Attachments)
	}
	if got.Attachments[0].MediaType != "image/png" {
		t.Errorf("mediaType = %q, want image/png", got.Attachments[0].MediaType)
	}
	// The wire carries base64, and it must round-trip to the exact bytes sent.
	raw, err := base64.StdEncoding.DecodeString(got.Attachments[0].Data)
	if err != nil {
		t.Fatalf("data is not base64: %v", err)
	}
	if string(raw) != "bytes" {
		t.Errorf("decoded = %q, want %q", raw, "bytes")
	}
}

// Every text format must arrive as text/plain, not its conventional type: the
// Anthropic provider maps exactly application/pdf and text/plain as documents,
// so text/csv would not be read as a document at all.
func TestReadMultipartTurnMediaTypes(t *testing.T) {
	cases := map[string]string{
		"a.pdf": "application/pdf",
		"a.png": "image/png", "a.jpg": "image/jpeg", "a.jpeg": "image/jpeg",
		"a.gif": "image/gif", "a.webp": "image/webp",
		"a.md": "text/plain", "a.txt": "text/plain", "a.csv": "text/plain",
		"a.tsv": "text/plain", "a.json": "text/plain", "a.yaml": "text/plain",
		"a.yml": "text/plain", "a.xml": "text/plain", "a.html": "text/plain",
		"a.rst": "text/plain",
	}
	for name, want := range cases {
		r := buildMultipart(t, map[string]string{"instruction": "hi"}, [][2]any{{name, "x"}})
		got, err := readMultipartTurn(r)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", name, err)
		}
		if len(got.Attachments) != 1 || got.Attachments[0].MediaType != want {
			t.Errorf("%s: mediaType = %+v, want %q", name, got.Attachments, want)
		}
	}
}

func TestReadMultipartTurnRejections(t *testing.T) {
	tests := []struct {
		name  string
		files [][2]any
		want  string
	}{
		{
			// The models do not read Office formats natively, so accepting one
			// would carry bytes no turn can use.
			name:  "unsupported type",
			files: [][2]any{{"brief.docx", "x"}},
			want:  "unsupported attachment type",
		},
		{
			name:  "over the per-file cap",
			files: [][2]any{{"huge.pdf", MaxChatAttachmentBytes + 1}},
			want:  "exceeds the 5 MiB per-file limit",
		},
		{
			// Two files under one name would collapse downstream: the agents
			// service dedupes by name against the conversation's history.
			name:  "duplicate name",
			files: [][2]any{{"brief.md", "a"}, {"brief.md", "b"}},
			want:  "attached twice",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := buildMultipart(t, map[string]string{"instruction": "hi"}, tc.files)
			_, err := readMultipartTurn(r)
			if err == nil {
				t.Fatal("expected a rejection")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want it to contain %q", err.Error(), tc.want)
			}
		})
	}
}

// The count cap must refuse the ELEVENTH file rather than buffering it first.
func TestReadMultipartTurnCountCap(t *testing.T) {
	files := make([][2]any, 0, MaxChatAttachmentCount+1)
	for i := 0; i <= MaxChatAttachmentCount; i++ {
		files = append(files, [2]any{string(rune('a'+i)) + ".md", "x"})
	}
	r := buildMultipart(t, map[string]string{"instruction": "hi"}, files)
	if _, err := readMultipartTurn(r); err == nil ||
		!strings.Contains(err.Error(), "at most 10 attachments per message") {
		t.Fatalf("err = %v, want the count cap", err)
	}
}

// The load-bearing cap. Four 5 MiB files each pass the per-file check and
// together are 20 MiB — over the 15 MiB the model's encoded budget allows — so
// without this the agent would silently drop the tail.
func TestReadMultipartTurnTotalCap(t *testing.T) {
	r := buildMultipart(t, map[string]string{"instruction": "hi"}, [][2]any{
		{"a.pdf", MaxChatAttachmentBytes},
		{"b.pdf", MaxChatAttachmentBytes},
		{"c.pdf", MaxChatAttachmentBytes},
		{"d.pdf", MaxChatAttachmentBytes},
	})
	_, err := readMultipartTurn(r)
	if err == nil {
		t.Fatal("expected the total cap to refuse the batch")
	}
	if !strings.Contains(err.Error(), "exceed the 15 MiB total for one message") {
		t.Errorf("error = %q, want the total cap", err.Error())
	}
}

// 15 MiB raw is exactly 20 MiB base64-encoded — the derivation the caps restate.
func TestTotalCapMatchesTheEncodedBudget(t *testing.T) {
	const encodedBudget = 20 << 20
	encoded := (MaxChatAttachmentTotalBytes + 2) / 3 * 4
	if encoded != encodedBudget {
		t.Errorf("%d raw bytes encode to %d, want exactly %d", MaxChatAttachmentTotalBytes, encoded, encodedBudget)
	}
}

// A crafted part must not smuggle a directory: the name reaches the journal and
// the model, so it is reduced to a bare base name.
func TestReadMultipartTurnStripsDirectories(t *testing.T) {
	for _, name := range []string{"../../etc/passwd.md", `C:\notes\brief.md`, "sub/dir/brief.md"} {
		r := buildMultipart(t, map[string]string{"instruction": "hi"}, [][2]any{{name, "x"}})
		got, err := readMultipartTurn(r)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", name, err)
		}
		if len(got.Attachments) != 1 {
			t.Fatalf("%s: attachments = %+v", name, got.Attachments)
		}
		if strings.ContainsAny(got.Attachments[0].Name, `/\`) {
			t.Errorf("%s: kept a path separator: %q", name, got.Attachments[0].Name)
		}
	}
}

// A body with no files at all is a valid multipart send — the console only
// reaches for multipart when there ARE files, but nothing downstream should
// break if it does otherwise.
func TestReadMultipartTurnNoFiles(t *testing.T) {
	r := buildMultipart(t, map[string]string{"instruction": "just text"}, nil)
	got, err := readMultipartTurn(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Instruction != "just text" || len(got.Attachments) != 0 {
		t.Errorf("got %+v", got)
	}
}

// Unknown fields are tolerated, as everywhere else on this body.
func TestReadMultipartTurnIgnoresUnknownFields(t *testing.T) {
	r := buildMultipart(t, map[string]string{"instruction": "hi", "somethingNew": "v"}, nil)
	got, err := readMultipartTurn(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Instruction != "hi" {
		t.Errorf("instruction = %q", got.Instruction)
	}
}
