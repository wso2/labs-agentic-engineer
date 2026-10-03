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
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/wso2/aep/ae-studio-tools/internal/gen"
	"github.com/wso2/aep/ae-studio-tools/internal/problem"
	"github.com/wso2/aep/ae-studio-tools/internal/turns"
)

// POST /internal/v1/repos/{owner}/{repo}/turns (start-repo-turn): aep-api
// starts a kickoff or plan turn, relayed to ae-design-agent over the Turn
// socket as an NDJSON stream (04 §5, 07 §5). The generated strict server
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

// startRepoTurn checks that the project is this repository's, then hands
// the turn to the relay. The project resolves through aep-api, in this pod's
// org; owner/repo must be its repository (GitHub names, compared
// case-insensitively), else 404 project_unknown: a caller cannot run a turn
// for a project against another repository.
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
	rep, err := s.projects.Resolve(r.Context(), req.Project)
	if err != nil {
		_ = filesProblem(r.Context(), "start-repo-turn", req.Project, err).write(w)
		return
	}
	if !strings.EqualFold(rep.Owner, r.PathValue("owner")) || !strings.EqualFold(rep.Repo, r.PathValue("repo")) {
		problem.Write(w, http.StatusNotFound, "project_unknown", "the project is not this repository's")
		return
	}
	s.turns.Serve(w, r, turns.Turn{ID: req.TurnID.String(), Project: req.Project, Kind: string(req.Kind), Body: body})
}

// StartRepoTurn satisfies the generated interface only: startRepoTurn
// serves the op on a raw route ahead of the generated one, so this is never
// called.
func (s internalServer) StartRepoTurn(context.Context, gen.StartRepoTurnRequestObject) (gen.StartRepoTurnResponseObject, error) {
	return nil, errors.New("start-repo-turn is served by the raw turns route")
}
