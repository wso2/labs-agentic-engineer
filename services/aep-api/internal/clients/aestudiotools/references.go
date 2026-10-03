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

package aestudiotools

// references.go — put-repo-references: a project's reference documents,
// streamed to the pod as the caller's multipart body (R21), never buffered.

import (
	"context"
	"io"
	"net/http"
	"sync/atomic"

	"github.com/wso2/aep/aep-api/internal/clients/aestudiotools/gen"
)

// References replaces a project's stored reference documents.
type References interface {
	PutReferences(ctx context.Context, ref RepoRef, contentType string, body io.Reader) error
}

// PutReferences streams body (a multipart upload of field `files`, typed by
// contentType) to the pod, which replaces the stored set. 400
// reference_rejected is ErrReferenceRejected (with the pod's detail), 503
// (disk_full) ErrAEStudioUnavailable; 403 owner_refused and 413 are permanent
// StatusErrors. The upload asks for 100 Continue, so a token refused before
// any byte was sent is refreshed and the same body sent once more; refused
// after the body went out, the token is dropped and the call is
// ErrAEStudioUnavailable (send again). PutReferences owns body: an io.Closer
// is closed on return, which releases a producer writing into an io.Pipe.
//
//deadcode:keep wired in Task 3.17 (reference uploads through the pod)
func (a *Adapter) PutReferences(ctx context.Context, ref RepoRef, contentType string, body io.Reader) error {
	if c, ok := body.(io.Closer); ok {
		defer func() { _ = c.Close() }()
	}
	if err := validRef(ref); err != nil {
		return err
	}
	ctx, cancel := a.unary(ctx)
	defer cancel()
	upload := &unsentBody{r: body}
	resp, err := a.send(ctx, ref.Org, "put-repo-references", func(ctx context.Context, c *gen.Client, impersonateOrg string, auth gen.RequestEditorFn) (*http.Response, error) {
		return c.PutRepoReferencesWithBody(ctx, ref.Owner, ref.Repo, &gen.PutRepoReferencesParams{XImpersonateOrg: impersonateOrg}, contentType, upload, auth, expectContinue)
	}, upload.unsent)
	if err != nil {
		return err
	}
	return resp.Body.Close()
}

// expectContinue makes the transport wait for the pod's 100 Continue (or its
// refusal) before it sends the upload.
//
//deadcode:keep wired in Task 3.17
func expectContinue(_ context.Context, req *http.Request) error {
	req.Header.Set("Expect", "100-continue")
	return nil
}

// unsentBody records whether the transport began reading the upload. It is
// deliberately not an io.Closer: the transport closes a request body after
// each send, and the body must survive a refused token to be sent again.
type unsentBody struct {
	r       io.Reader
	touched atomic.Bool
}

//deadcode:keep wired in Task 3.17
func (b *unsentBody) Read(p []byte) (int, error) {
	b.touched.Store(true)
	return b.r.Read(p)
}

// unsent is true while no byte of the upload has been read.
//
//deadcode:keep wired in Task 3.17
func (b *unsentBody) unsent() bool { return !b.touched.Load() }
