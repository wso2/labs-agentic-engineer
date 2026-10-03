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

package edge

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/wso2/aep/ae-studio-tools/internal/files"
	"github.com/wso2/aep/ae-studio-tools/internal/gen/filessock"
	"github.com/wso2/aep/ae-studio-tools/internal/problem"
)

// The Files socket is served contract-first from
// packages/contracts/sockets/ae-studio/files: the generated strict server in
// internal/gen/filessock, behind a request validator over the same contract.
// ae-collab is its only caller (the socket's directory is mounted into
// ae-collab and this container only, and the socket is 0660), so there is no
// token gate. The project is the only address a request carries: every
// operation resolves it through aep-api (files.Reader), and the contract has
// no owner or repo field, so the validator refuses a request naming one.

const (
	// filesSocketMode lets the pod's shared group (fsGroup) connect.
	filesSocketMode fs.FileMode = 0o660
	// filesSocketBodyBytes caps a request body; only apply has one (04 §7).
	filesSocketBodyBytes int64 = 25 << 20
	// filesSocketRequestBudget bounds one Files socket request, so the pod
	// always answers before its caller gives up: it nests every aep-api call
	// (15 s each, platform.aepAPITimeout; an apply makes at most two: the
	// resolve and the completions) and the git work after them, and ends
	// inside ae-collab's per-call deadline (REQUEST_TIMEOUT_MS, 45 s,
	// ae-collab/src/files-client.ts); raise both together. A cold clone runs
	// detached from the request (repo.Engine.ensureMirror), so a request that
	// spends its budget waiting on one leaves the clone running for the next.
	filesSocketRequestBudget = 40 * time.Second
)

// ListenFilesSocket binds the Files socket at path. A socket file left by a
// previous run is removed first; any other file at path is an error, never
// removed. The socket is made 0660; closing the listener unlinks it.
func ListenFilesSocket(path string) (net.Listener, error) {
	if err := removeStaleSocket(path); err != nil {
		return nil, err
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("files socket: listen: %w", err)
	}
	if err := os.Chmod(path, filesSocketMode); err != nil {
		_ = ln.Close()
		return nil, fmt.Errorf("files socket: chmod: %w", err)
	}
	return ln, nil
}

// removeStaleSocket removes a socket file at path; nothing there is fine.
func removeStaleSocket(path string) error {
	fi, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("files socket: stat: %w", err)
	}
	if fi.Mode().Type() != fs.ModeSocket {
		return fmt.Errorf("files socket: %s exists and is not a socket", path)
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("files socket: remove stale socket: %w", err)
	}
	return nil
}

// filesSocketHandler is validator → generated server. A path or method the
// contract does not declare is 404 at the validator; a request that does not
// match its operation (an unknown field included) is 400 path_invalid, the
// contract's code for a refused request.
func filesSocketHandler(a files.Applier) http.Handler {
	strict := filessock.NewStrictHandlerWithOptions(filesSocketServer{applier: a}, nil, filessock.StrictHTTPServerOptions{
		RequestErrorHandlerFunc:  writeFilesSocketRequestError,
		ResponseErrorHandlerFunc: writeFilesSocketResponseError,
	})
	mux := http.NewServeMux()
	filessock.HandlerWithOptions(strict, filessock.StdHTTPServerOptions{
		BaseRouter:       mux,
		ErrorHandlerFunc: writeFilesSocketRequestError,
	})
	mux.Handle("/", http.HandlerFunc(notFound))
	return requestValidator(mustRouter("files socket", filessock.GetSpec).FindRoute, "path_invalid", mux)
}

// filesSocketServer implements the Files socket operations: reads through
// the Applier's Reader, the apply through the Applier.
type filesSocketServer struct {
	applier files.Applier
}

var _ filessock.StrictServerInterface = filesSocketServer{}

// LookupProject answers that the project is known, with aep-api's repository
// for it and the branch tip.
func (s filesSocketServer) LookupProject(ctx context.Context, req filessock.LookupProjectRequestObject) (filessock.LookupProjectResponseObject, error) {
	l, err := s.applier.Reader.Lookup(ctx, req.ProjectName)
	if err != nil {
		return filesProblem(ctx, "lookup", req.ProjectName, err), nil
	}
	return filessock.LookupProject200JSONResponse{Known: filessock.True, Owner: l.Owner, Repo: l.Repo, HeadSha: l.HeadSHA}, nil
}

// ReadProjectBundle reads every readable file under prefix at the tip: the
// Room's seed, whose blob shas are its later baseShas.
func (s filesSocketServer) ReadProjectBundle(ctx context.Context, req filessock.ReadProjectBundleRequestObject) (filessock.ReadProjectBundleResponseObject, error) {
	b, err := s.applier.Reader.Bundle(ctx, req.ProjectName, req.Params.Prefix, "")
	if err != nil {
		return filesProblem(ctx, "bundle", req.ProjectName, err), nil
	}
	out := filessock.ReadProjectBundle200JSONResponse{CommitSha: b.CommitSHA, Files: make([]filessock.FileContent, 0, len(b.Files))}
	for _, f := range b.Files {
		out.Files = append(out.Files, filessock.FileContent{Path: f.Path, Content: f.Content, Sha: f.SHA})
	}
	return out, nil
}

// ApplyProjectFiles applies the writes and deletes as one commit. A failed
// baseSha is 409 with the conflicts (nothing applied); warnings pass through
// on 200 as {path, message}.
func (s filesSocketServer) ApplyProjectFiles(ctx context.Context, req filessock.ApplyProjectFilesRequestObject) (filessock.ApplyProjectFilesResponseObject, error) {
	res, conflicts, err := s.applier.Apply(ctx, req.ProjectName, applyRequest(req.Body))
	if errors.Is(err, files.ErrApplyConflict) {
		out := filessock.ApplyProjectFiles409JSONResponse{Code: filessock.ApplyConflictsCodeConflict, Conflicts: make([]filessock.Conflict, 0, len(conflicts))}
		for _, c := range conflicts {
			out.Conflicts = append(out.Conflicts, filessock.Conflict{Path: c.Path, BaseSha: c.BaseSHA, CurrentSha: c.CurrentSHA})
		}
		return out, nil
	}
	if err != nil {
		return filesProblem(ctx, "apply", req.ProjectName, err), nil
	}
	out := filessock.ApplyProjectFiles200JSONResponse{
		CommitSha: res.CommitSHA,
		Changed:   res.Changed,
		Files:     make([]filessock.AppliedFile, 0, len(res.Files)),
		Warnings:  make([]filessock.ApplyWarning, 0, len(res.Warnings)),
	}
	for _, f := range res.Files {
		out.Files = append(out.Files, filessock.AppliedFile{Path: f.Path, Sha: f.SHA})
	}
	for _, w := range res.Warnings {
		// The contract's warning is {path, message}; the core's code stays
		// in the pod.
		out.Warnings = append(out.Warnings, filessock.ApplyWarning{Path: w.Path, Message: w.Message})
	}
	return out, nil
}

// applyRequest maps the socket's body to the apply core's request. The
// validator guarantees a body with every required field.
func applyRequest(body *filessock.ApplyRequest) files.ApplyRequest {
	out := files.ApplyRequest{
		Message: body.Message,
		Writes:  make([]files.WriteOp, 0, len(body.Writes)),
		Deletes: make([]files.DeleteOp, 0, len(body.Deletes)),
	}
	for _, w := range body.Writes {
		out.Writes = append(out.Writes, files.WriteOp{Path: w.Path, Content: w.Content, BaseSHA: w.BaseSha})
	}
	for _, d := range body.Deletes {
		out.Deletes = append(out.Deletes, files.DeleteOp{Path: d.Path, BaseSHA: d.BaseSha})
	}
	return out
}

// writeFilesSocketRequestError answers a request the generated binder could
// not parse.
func writeFilesSocketRequestError(w http.ResponseWriter, _ *http.Request, _ error) {
	problem.Write(w, http.StatusBadRequest, "path_invalid", "the request does not match the contract")
}

// writeFilesSocketResponseError answers a handler error no typed response
// covers. The handlers return none, so this is a response that failed to
// encode; its error is not logged, since a Files error can carry git text.
func writeFilesSocketResponseError(w http.ResponseWriter, r *http.Request, _ error) {
	slog.Error("files_socket.handler_failed", "path", r.URL.Path)
	problem.Write(w, http.StatusInternalServerError, "internal_error", "the request could not be completed")
}

func (p problemResponse) VisitLookupProjectResponse(w http.ResponseWriter) error { return p.write(w) }

func (p problemResponse) VisitReadProjectBundleResponse(w http.ResponseWriter) error {
	return p.write(w)
}

func (p problemResponse) VisitApplyProjectFilesResponse(w http.ResponseWriter) error {
	return p.write(w)
}
