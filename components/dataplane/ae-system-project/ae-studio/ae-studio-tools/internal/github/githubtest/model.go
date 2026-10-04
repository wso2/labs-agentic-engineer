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

package githubtest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
)

// The stub's model of GitHub: repositories, issues and repository hooks,
// seeded by a test and changed by the calls the client makes. It answers
// only the routes below, the way GitHub does (status, body shape, the 422
// texts the client recognises); anything else falls through to the stub's
// 404. Owner and repository names compare case-insensitively, as GitHub's do.
//
//	POST   /orgs/{owner}/repos              create (422 "name already exists")
//	GET    /repos/{owner}/{repo}            the repository
//	GET    /repos/{owner}/{repo}/issues     seeded issues, labels filter (AND)
//	GET    /repos/{owner}/{repo}/issues/{n} one issue, else 404
//	POST   /repos/{owner}/{repo}/hooks      register (422 "Hook already exists")
//	GET    /repos/{owner}/{repo}/hooks      every hook, one page
//	PATCH  /repos/{owner}/{repo}/hooks/{id} replace the events
//	DELETE /repos/{owner}/{repo}/hooks/{id} remove, else 404

type model struct {
	// repos maps a lower-cased "owner/repo" to its seeded repository.
	repos  map[string]*seededRepo
	nextID int64
}

type seededRepo struct {
	owner, name string
	issues      map[int]seededIssue
	hooks       []*seededHook
}

type seededIssue struct {
	title  string
	labels []string
}

type seededHook struct {
	id     int64
	url    string
	events []string
}

func newModel() model {
	return model{repos: map[string]*seededRepo{}, nextID: 1000}
}

func repoKey(owner, repo string) string {
	return strings.ToLower(owner + "/" + repo)
}

// SeedRepo makes owner/repo exist (default branch main, no hooks).
func (s *Stub) SeedRepo(owner, repo string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seedRepoLocked(owner, repo)
}

func (s *Stub) seedRepoLocked(owner, repo string) *seededRepo {
	k := repoKey(owner, repo)
	if r, ok := s.model.repos[k]; ok {
		return r
	}
	r := &seededRepo{owner: owner, name: repo, issues: map[int]seededIssue{}}
	s.model.repos[k] = r
	return r
}

// SeedIssue makes issue number of owner/repo exist, open, with title and
// labels (the repository is seeded too).
func (s *Stub) SeedIssue(owner, repo string, number int, title string, labels []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seedRepoLocked(owner, repo).issues[number] = seededIssue{title: title, labels: slices.Clone(labels)}
}

// HookEvents is the event list of hook id on owner/repo, nil when there is
// no such hook.
func (s *Stub) HookEvents(owner, repo string, id int64) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r, ok := s.model.repos[repoKey(owner, repo)]; ok {
		for _, h := range r.hooks {
			if h.id == id {
				return slices.Clone(h.events)
			}
		}
	}
	return nil
}

// serveModel answers r from the model and reports whether it did.
func (s *Stub) serveModel(w http.ResponseWriter, r *http.Request, body []byte) bool {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case len(parts) == 3 && parts[0] == "orgs" && parts[2] == "repos" && r.Method == http.MethodPost:
		s.createRepo(w, parts[1], body)
		return true
	case len(parts) < 3 || parts[0] != "repos":
		return false
	}
	repo, ok := s.model.repos[repoKey(parts[1], parts[2])]
	if !ok {
		return false
	}
	rest := parts[3:]
	switch {
	case len(rest) == 0 && r.Method == http.MethodGet:
		writeValue(w, http.StatusOK, repoJSON(repo))
	case len(rest) == 1 && rest[0] == "issues" && r.Method == http.MethodGet:
		s.listIssues(w, r, repo)
	case len(rest) == 2 && rest[0] == "issues" && r.Method == http.MethodGet:
		n, err := strconv.Atoi(rest[1])
		issue, found := repo.issues[n]
		if err != nil || !found {
			return false
		}
		writeValue(w, http.StatusOK, issueJSON(n, issue))
	case len(rest) == 1 && rest[0] == "hooks" && r.Method == http.MethodPost:
		s.registerHook(w, repo, body)
	case len(rest) == 1 && rest[0] == "hooks" && r.Method == http.MethodGet:
		hooks := make([]map[string]any, 0, len(repo.hooks))
		for _, h := range repo.hooks {
			hooks = append(hooks, map[string]any{"id": h.id, "events": h.events, "config": map[string]string{"url": h.url}})
		}
		writeValue(w, http.StatusOK, hooks)
	case len(rest) == 2 && rest[0] == "hooks" && (r.Method == http.MethodPatch || r.Method == http.MethodDelete):
		return s.changeHook(w, r.Method, repo, rest[1], body)
	default:
		return false
	}
	return true
}

func (s *Stub) createRepo(w http.ResponseWriter, owner string, body []byte) {
	var req struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(body, &req); err != nil || req.Name == "" {
		writeJSON(http.StatusUnprocessableEntity, `{"message":"Validation Failed"}`)(w, nil)
		return
	}
	if _, taken := s.model.repos[repoKey(owner, req.Name)]; taken {
		writeJSON(http.StatusUnprocessableEntity, `{"message":"Repository creation failed.","errors":[{"resource":"Repository","code":"custom","field":"name","message":"name already exists on this account"}]}`)(w, nil)
		return
	}
	writeValue(w, http.StatusCreated, repoJSON(s.seedRepoLocked(owner, req.Name)))
}

func repoJSON(r *seededRepo) map[string]any {
	return map[string]any{
		"name":           r.name,
		"full_name":      r.owner + "/" + r.name,
		"owner":          map[string]string{"login": r.owner},
		"clone_url":      fmt.Sprintf("https://github.com/%s/%s.git", r.owner, r.name),
		"default_branch": "main",
	}
}

func (s *Stub) listIssues(w http.ResponseWriter, r *http.Request, repo *seededRepo) {
	var want []string
	if l := r.URL.Query().Get("labels"); l != "" {
		want = strings.Split(l, ",")
	}
	out := []map[string]any{}
	if p := r.URL.Query().Get("page"); p == "" || p == "1" {
		numbers := make([]int, 0, len(repo.issues))
		for n := range repo.issues {
			numbers = append(numbers, n)
		}
		slices.Sort(numbers)
		slices.Reverse(numbers) // newest first
		for _, n := range numbers {
			issue := repo.issues[n]
			if !containsAll(issue.labels, want) {
				continue
			}
			out = append(out, issueJSON(n, issue))
		}
	}
	writeValue(w, http.StatusOK, out)
}

func containsAll(have, want []string) bool {
	for _, l := range want {
		if !slices.Contains(have, l) {
			return false
		}
	}
	return true
}

func issueJSON(n int, issue seededIssue) map[string]any {
	labels := make([]map[string]string, 0, len(issue.labels))
	for _, l := range issue.labels {
		labels = append(labels, map[string]string{"name": l})
	}
	return map[string]any{
		"number": n, "title": issue.title, "body": "", "state": "open",
		"html_url": fmt.Sprintf("https://github.com/issues/%d", n), "labels": labels,
	}
}

func (s *Stub) registerHook(w http.ResponseWriter, repo *seededRepo, body []byte) {
	var req struct {
		Events []string `json:"events"`
		Config struct {
			URL string `json:"url"`
		} `json:"config"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSON(http.StatusUnprocessableEntity, `{"message":"Validation Failed"}`)(w, nil)
		return
	}
	for _, h := range repo.hooks {
		if h.url == req.Config.URL {
			writeJSON(http.StatusUnprocessableEntity, `{"message":"Validation Failed","errors":[{"resource":"Hook","code":"custom","message":"Hook already exists on this repository"}]}`)(w, nil)
			return
		}
	}
	s.model.nextID++
	h := &seededHook{id: s.model.nextID, url: req.Config.URL, events: req.Events}
	repo.hooks = append(repo.hooks, h)
	writeValue(w, http.StatusCreated, map[string]any{"id": h.id, "events": h.events})
}

func (s *Stub) changeHook(w http.ResponseWriter, method string, repo *seededRepo, idText string, body []byte) bool {
	id, err := strconv.ParseInt(idText, 10, 64)
	if err != nil {
		return false
	}
	i := slices.IndexFunc(repo.hooks, func(h *seededHook) bool { return h.id == id })
	if i < 0 {
		return false
	}
	if method == http.MethodDelete {
		repo.hooks = slices.Delete(repo.hooks, i, i+1)
		w.WriteHeader(http.StatusNoContent)
		return true
	}
	var req struct {
		Events []string `json:"events"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSON(http.StatusUnprocessableEntity, `{"message":"Validation Failed"}`)(w, nil)
		return true
	}
	repo.hooks[i].events = req.Events
	writeValue(w, http.StatusOK, map[string]any{"id": id, "events": req.Events})
	return true
}

func writeValue(w http.ResponseWriter, status int, v any) {
	b, _ := json.Marshal(v)
	writeJSON(status, string(b))(w, nil)
}
