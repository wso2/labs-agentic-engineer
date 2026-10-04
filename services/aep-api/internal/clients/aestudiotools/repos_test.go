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

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// platformHookEvents are the four events the platform's repository hook carries.
var platformHookEvents = []string{"pull_request", "push", "issue_comment", "issues"}

func TestRepoAndHookOps_RequestAndReply(t *testing.T) {
	none := func(err error) (any, error) { return nil, err }
	runOps(t, []opCase{
		{
			name: "create repo", method: "POST", path: "/repos", status: 201,
			body:  `{"owner":"acme","name":"greeter","private":true,"description":"d","adoptExisting":true}`,
			reply: `{"owner":"acme","repo":"greeter","cloneUrl":"https://github.com/acme/greeter.git","defaultBranch":"main"}`,
			want:  "https://github.com/acme/greeter.git",
			call: func(ctx context.Context, a *Adapter) (any, error) {
				return a.CreateOrgRepo(ctx, RepoRef{Org: "default", Owner: "acme", Repo: "greeter"},
					sourcecontrol.CreateOrgRepoRequest{Private: true, Description: "d", AdoptExisting: true})
			},
		},
		{
			name: "trash repo", method: "POST", path: "/trash", status: 204, body: `{"owner":"acme","repo":"greeter"}`,
			call: func(ctx context.Context, a *Adapter) (any, error) { return none(a.TrashRepo(ctx, trunkRef)) },
		},
		{
			name: "register hook", method: "POST", path: "/repos/acme/greeter/hooks",
			body:  `{"events":["pull_request","push","issue_comment","issues"]}`,
			reply: `{"id":42}`, want: int64(42),
			call: func(ctx context.Context, a *Adapter) (any, error) {
				return a.RegisterWebhook(ctx, trunkRef, platformHookEvents)
			},
		},
		{
			name: "update hook events", method: "PATCH", path: "/repos/acme/greeter/hooks/42", status: 204,
			body: `{"events":["pull_request","push","issue_comment","issues"]}`,
			call: func(ctx context.Context, a *Adapter) (any, error) {
				return none(a.UpdateWebhookEvents(ctx, trunkRef, 42, platformHookEvents))
			},
		},
		{
			name: "delete hook", method: "DELETE", path: "/repos/acme/greeter/hooks/42", status: 204,
			call: func(ctx context.Context, a *Adapter) (any, error) { return none(a.DeleteWebhook(ctx, trunkRef, 42)) },
		},
		{
			name: "mirror skills", method: "POST", path: "/repos/acme/greeter/skills-mirror", query: "defaultBranch=trunk",
			body:  `{"skillsRepo":{"owner":"acme","repo":"skills","defaultBranch":"main"},"pinned":["a"]}`,
			reply: `{"commitSha":"` + sha40 + `","changed":true}`,
			want:  sourcecontrol.CommitResult{CommitSHA: sha40, Changed: true},
			call: func(ctx context.Context, a *Adapter) (any, error) {
				return a.MirrorSkills(ctx, trunkRef, RepoRef{Org: "default", Owner: "acme", Repo: "skills", DefaultBranch: "main"}, []string{"a"})
			},
		},
		{
			name: "mirror skills with no pins", method: "POST", path: "/repos/acme/greeter/skills-mirror", query: "defaultBranch=trunk",
			body:  `{"skillsRepo":{"owner":"acme","repo":"skills","defaultBranch":"main"},"pinned":[]}`,
			reply: `{"commitSha":"` + sha40 + `","changed":false}`,
			want:  sourcecontrol.CommitResult{CommitSHA: sha40},
			call: func(ctx context.Context, a *Adapter) (any, error) {
				return a.MirrorSkills(ctx, trunkRef, RepoRef{Org: "default", Owner: "acme", Repo: "skills", DefaultBranch: "main"}, nil)
			},
		},
	})
}

// The platform hook carries exactly its four events; anything else is
// refused before a request leaves (the pod's enum would answer 400).
func TestHookOps_RefuseAnUnknownEventWithoutACall(t *testing.T) {
	p, srv := newPodStub(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) })
	a := newAdapter(t, fixedTarget(srv.URL, "ou-123"), &countingTokens{})
	if _, err := a.RegisterWebhook(context.Background(), trunkRef, []string{"push", "release"}); err == nil {
		t.Fatal("want a refusal of release")
	}
	if err := a.UpdateWebhookEvents(context.Background(), trunkRef, 42, []string{"*"}); err == nil {
		t.Fatal("want a refusal of *")
	}
	if len(p.requests()) != 0 {
		t.Fatal("no request may leave")
	}
}

// A mirror that lost a race (the pod's 409 conflict or not_fast_forward) is
// a commit conflict to the best-effort caller; the skills repo must be the
// project's org.
func TestMirrorSkills_Refusals(t *testing.T) {
	for _, code := range []string{"conflict", "not_fast_forward"} {
		_, srv := newPodStub(t, func(w http.ResponseWriter, _ *http.Request) { writeProblem(w, http.StatusConflict, code, "") })
		a := newAdapter(t, fixedTarget(srv.URL, "ou-123"), &countingTokens{})
		_, err := a.MirrorSkills(context.Background(), trunkRef, RepoRef{Org: "default", Owner: "acme", Repo: "skills"}, []string{"a"})
		if !errors.Is(err, sourcecontrol.ErrCommitConflict) || sourcecontrol.IsPermanent(err) {
			t.Errorf("%s: err = %v, want a retryable ErrCommitConflict", code, err)
		}
	}
	p, srv := newPodStub(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })
	a := newAdapter(t, fixedTarget(srv.URL, "ou-123"), &countingTokens{})
	if _, err := a.MirrorSkills(context.Background(), trunkRef, RepoRef{Org: "other", Owner: "acme", Repo: "skills"}, nil); err == nil || len(p.requests()) != 0 {
		t.Fatalf("err = %v requests = %d, want a refusal without a call", err, len(p.requests()))
	}
}

func TestCreateOrgRepo_NameConflict(t *testing.T) {
	_, srv := newPodStub(t, func(w http.ResponseWriter, _ *http.Request) {
		writeProblem(w, http.StatusConflict, "repo_name_conflict", "")
	})
	a := newAdapter(t, fixedTarget(srv.URL, "ou-123"), &countingTokens{})
	if _, err := a.CreateOrgRepo(context.Background(), trunkRef, sourcecontrol.CreateOrgRepoRequest{Private: true}); !sourcecontrol.IsRepoNameConflict(err) {
		t.Fatalf("err = %v, want ErrRepoNameConflict", err)
	}
}
