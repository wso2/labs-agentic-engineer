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
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/wso2/aep/aep-api/internal/gen"
	"github.com/wso2/aep/aep-api/internal/platform/apierr"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// A wrapped Workspace.Mutate CAS-exhaustion (ErrRefNotFastForward) is a
// concurrent-write conflict, so mapFilesError must render it as a 409 — not
// the 500 the default arm would otherwise produce. (Ported from the files
// feature's Huma-era internal test at the contract-first cutover.)
func TestMapFilesError_CASExhaustionMapsTo409(t *testing.T) {
	err := mapFilesError(fmt.Errorf("apply: mutate: %w", sourcecontrol.ErrRefNotFastForward))
	var ae *apierr.Error
	if !errors.As(err, &ae) {
		t.Fatalf("mapped error %T is not an *apierr.Error", err)
	}
	if ae.Status != http.StatusConflict || ae.Code != apierr.CodeConflict {
		t.Fatalf("status/code = %d/%s, want 409/%s (CAS exhaustion is a retryable conflict)",
			ae.Status, ae.Code, apierr.CodeConflict)
	}
}

// The Files API is text-only: content is already the file's bytes and must
// survive byte-identically. The base64 half that once rode here went out with
// reference documents (console ADR-0017) — they are stored off-git now, so
// nothing binary reaches this endpoint at all.
func TestApplyRequestFromWire_PassesContentThroughUnchanged(t *testing.T) {
	const content = "# Requirements\n\nplain markdown — with unicode ✅\n"
	body := gen.ApplyRequest{Writes: []gen.WriteOp{{
		Path: "specs/requirements/requirements.md", Content: content,
	}}}

	got := applyRequestFromWire(body)
	if len(got.Writes) != 1 {
		t.Fatalf("got %d writes, want 1", len(got.Writes))
	}
	if got.Writes[0].Content != content {
		t.Fatalf("content = %q, want it unchanged %q", got.Writes[0].Content, content)
	}
}
