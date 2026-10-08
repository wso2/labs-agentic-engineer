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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/wso2/aep/ae-studio-tools/internal/gen"
	"github.com/wso2/aep/ae-studio-tools/internal/problem"
	"github.com/wso2/aep/ae-studio-tools/internal/repo"
	"github.com/wso2/aep/ae-studio-tools/internal/turns"
)

// POST /internal/v1/repos/{owner}/{repo}/turns (start-repo-turn): aep-api
// starts a kickoff or plan turn, relayed to ae-design-agent over the Turn
// socket as an NDJSON stream. The generated strict server
// cannot stream with a flush per frame and notice the caller leaving, so
// the op is a raw route behind the group's cap → gate → validator chain
// (internalHandler); the request body is small JSON and is validated like
// every other.

// startRepoTurnPattern is the op's ServeMux pattern: the generated router's
// own, so the raw route serves exactly what the contract declares.
const startRepoTurnPattern = http.MethodPost + " " + internalV1 + "/repos/{owner}/{repo}/turns"

// TurnRelay streams one turn aep-api starts (turns.Relay).
type TurnRelay interface {
	Serve(w http.ResponseWriter, r *http.Request, t turns.Turn)
}

// startRepoTurn checks that the project is this repository's, resolves a
// plan turn's at to its commit, then hands the turn to the relay. The
// project resolves through aep-api, in this pod's org; owner/repo must be its
// repository (GitHub names, compared case-insensitively), else 404
// project_unknown: a caller cannot run a turn for a project against another
// repository.
func (s internalServer) startRepoTurn(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeBodyTooLarge(w)
			return
		}
		writeRequestError(w, r, err)
		return
	}
	var req gen.TurnRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeRequestError(w, r, err)
		return
	}
	if req.At != "" && req.Kind != gen.TurnRequestKindPlan {
		problem.Write(w, http.StatusBadRequest, "validation_failed", "only a plan turn reads the repository at a pinned commit")
		return
	}
	rep, err := s.projects.Resolve(r.Context(), req.Project)
	if err != nil {
		_ = filesProblem(r.Context(), "start-repo-turn", req.Project, err).write(w)
		return
	}
	owner, name := r.PathValue("owner"), r.PathValue("repo")
	if !strings.EqualFold(rep.Owner, owner) || !strings.EqualFold(rep.Repo, name) {
		problem.Write(w, http.StatusNotFound, "project_unknown", "the project is not this repository's")
		return
	}
	if req.At != "" {
		sha, err := s.ResolveAt(r.Context(), owner, name, rep.DefaultBranch, req.At)
		if err != nil {
			writeGitProblem(w, r, owner, name, err)
			return
		}
		if body, err = pinTurnAt(body, sha); err != nil {
			writeResponseError(w, r, err)
			return
		}
	}
	s.turns.Serve(w, r, turns.Turn{ID: req.TurnID.String(), Project: req.Project, Kind: string(req.Kind), Body: body})
}

// pinTurnAt is the request with its at replaced by sha, the commit it names:
// the Turn socket's at is a resolved commit, so the agent never resolves a
// ref. Every other member is relayed as sent.
func pinTurnAt(body []byte, sha string) (turns.Body, error) {
	var members map[string]json.RawMessage
	if err := json.Unmarshal(body, &members); err != nil {
		return nil, err
	}
	members["at"] = json.RawMessage(strconv.Quote(sha))
	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetEscapeHTML(false) // the text members stay as sent
	if err := enc.Encode(members); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(out.Bytes(), []byte("\n")), nil
}

// writeGitProblem answers a failure to resolve a turn's at as the git ops
// answer it (repo.Problem).
func writeGitProblem(w http.ResponseWriter, r *http.Request, owner, name string, err error) {
	p, perr := repo.Problem(r.Context(), "start-repo-turn", owner, name, err)
	if perr != nil {
		writeResponseError(w, r, perr)
		return
	}
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(p.Status)
	_ = json.NewEncoder(w).Encode(p)
}

// StartRepoTurn satisfies the generated interface only: startRepoTurn
// serves the op on a raw route ahead of the generated one, so this is never
// called.
func (s internalServer) StartRepoTurn(context.Context, gen.StartRepoTurnRequestObject) (gen.StartRepoTurnResponseObject, error) {
	return nil, errors.New("start-repo-turn is served by the raw turns route")
}
