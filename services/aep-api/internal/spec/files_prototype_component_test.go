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

// The console reads a web-application's prototype through the existing
// read-file operation (no dedicated endpoint), and a feedback turn's CAS rides
// the blob SHA that read returns. This pins that half of the /prototype
// scenario on the real Files API over real git: the shared expense-approval
// fixture saved at its component path reads back byte-identical, with the git
// blob SHA of those bytes — the same SHA the save reported.
package spec_test

import (
	"crypto/sha1" //nolint:gosec // git's object id, not a security hash
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"testing"

	"github.com/wso2/aep/aep-api/internal/spec"
)

func gitBlobSHA(content string) string {
	sum := sha1.Sum([]byte(fmt.Sprintf("blob %d\x00%s", len(content), content))) //nolint:gosec
	return hex.EncodeToString(sum[:])
}

func TestReadFile_PrototypeAtComponentPath_ReturnsBodyAndSHA(t *testing.T) {
	raw, err := os.ReadFile("../../../../packages/prototype-model/fixtures/expense-approval.json")
	if err != nil {
		t.Fatalf("read the shared fixture — layout drift?: %v", err)
	}
	fixture := string(raw)
	var doc struct {
		Component string `json:"component"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	path := "specs/design/components/" + doc.Component + "/prototype.json"

	r := newFilesRig(t, map[string]string{
		"specs/design/components/" + doc.Component + "/design.json": `{"name":"` + doc.Component + `","type":"web-application"}`,
	})
	saved := r.apply(mustJSON(t, spec.ApplyRequest{Writes: []spec.WriteOp{{Path: path, Content: fixture}}}))
	if saved.Code != http.StatusOK {
		t.Fatalf("save code %d: %s", saved.Code, saved.Body.String())
	}
	var res spec.ApplyResult
	if err := json.Unmarshal(saved.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode save: %v", err)
	}

	rec := r.get(apiBase + "/" + path)
	if rec.Code != http.StatusOK {
		t.Fatalf("read code %d: %s", rec.Code, rec.Body.String())
	}
	var fc spec.FileContent
	if err := json.Unmarshal(rec.Body.Bytes(), &fc); err != nil {
		t.Fatalf("decode read: %v", err)
	}
	if fc.Path != path || fc.Content != fixture {
		t.Fatalf("read %q: the prototype must come back byte-identical", fc.Path)
	}
	if want := gitBlobSHA(fixture); fc.SHA != want {
		t.Fatalf("sha = %s, want the git blob sha %s", fc.SHA, want)
	}
	if len(res.Files) != 1 || res.Files[0].Path != path || res.Files[0].SHA != fc.SHA {
		t.Fatalf("save reported %+v, want %s at %s", res.Files, fc.SHA, path)
	}
}
