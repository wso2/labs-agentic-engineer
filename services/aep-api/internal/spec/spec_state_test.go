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

package spec

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime/multipart"
	"slices"
	"testing"

	"github.com/wso2/aep/aep-api/internal/clients/aestudiotools/aestudiotest"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// The spec workspace lists the source documents the user attached (N5), by
// the names the pod stored them under; none attached is an empty list.
func TestSpecState_Documents(t *testing.T) {
	t.Parallel()
	r := newRig(t, validSpecSeed())
	ctx := context.Background()
	st, err := r.svc.SpecState(ctx, r.org, r.proj)
	if err != nil || st.Documents == nil || len(st.Documents) != 0 {
		t.Fatalf("none attached: %#v, %v", st.Documents, err)
	}

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for _, n := range []string{"Brief.PDF", "notes.md"} {
		w, _ := mw.CreateFormFile("files", n)
		_, _ = io.WriteString(w, n)
	}
	_ = mw.Close()
	if err := r.pod.PutReferences(ctx, r.repoRef(), mw.FormDataContentType(), &buf); err != nil {
		t.Fatal(err)
	}
	st, err = r.svc.SpecState(ctx, r.org, r.proj)
	if err != nil || !slices.Equal(st.Documents, []string{"brief.pdf", "notes.md"}) {
		t.Fatalf("documents = %v, %v", st.Documents, err)
	}
}

// A list the pod could not answer is the state's error, never an empty
// list: "no documents" would be a wrong answer, not a degraded one.
func TestSpecState_DocumentsUnreadable(t *testing.T) {
	t.Parallel()
	r := newRig(t, validSpecSeed())
	r.pod.FailOp(aestudiotest.OpListReferences, sourcecontrol.ErrAEStudioUnavailable)
	if _, err := r.svc.SpecState(context.Background(), r.org, r.proj); !errors.Is(err, sourcecontrol.ErrAEStudioUnavailable) {
		t.Fatalf("err = %v, want the pod's unavailability", err)
	}
}
