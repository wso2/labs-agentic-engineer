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

package codingagent

import (
	"context"
	"sync"

	"github.com/wso2/aep/aep-api/internal/contracts/taskmeta"
	"github.com/wso2/aep/aep-api/models"
)

// fakeRepos returns a fixed project repo (its RepoSlug drives build-secret
// staging). A nil repo simulates "no repo row".
type fakeRepos struct{ repo *models.GitRepository }

func (f fakeRepos) GetRepo(context.Context, string, string) (*models.GitRepository, error) {
	return f.repo, nil
}

// fakeStager records StageBuildSecret calls and returns a scripted (ref, err).
type fakeStager struct {
	ref  string
	err  error
	mu   sync.Mutex
	runs []string // workflowRunName per call
}

func (f *fakeStager) StageBuildSecret(_ context.Context, _, _, runName string) (string, error) {
	f.mu.Lock()
	f.runs = append(f.runs, runName)
	f.mu.Unlock()
	return f.ref, f.err
}

func (f *fakeStager) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.runs)
}

// fakeEnsurer records EnsureComponent pre-flight calls and can be scripted to
// fail (a provisioning failure must block the coding dispatch).
type fakeEnsurer struct {
	err  error
	mu   sync.Mutex
	args [][3]string // (org, project, component) per call
}

func (f *fakeEnsurer) EnsureComponent(_ context.Context, org, project, component string) error {
	f.mu.Lock()
	f.args = append(f.args, [3]string{org, project, component})
	f.mu.Unlock()
	return f.err
}

func (f *fakeEnsurer) calls() [][3]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([][3]string{}, f.args...)
}

// fakeRuntimeConfig records EmitForComponent calls at the ensure pre-flight and
// can be scripted to fail (emission is best-effort — a failure must not block the
// coding dispatch). Satisfies codingagent.ComponentRuntimeConfigEmitter.
type fakeRuntimeConfig struct {
	err  error
	mu   sync.Mutex
	args [][3]string // (org, project, component) per call
}

func (f *fakeRuntimeConfig) EmitForComponent(_ context.Context, org, project, component string) error {
	f.mu.Lock()
	f.args = append(f.args, [3]string{org, project, component})
	f.mu.Unlock()
	return f.err
}

func (f *fakeRuntimeConfig) calls() [][3]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([][3]string{}, f.args...)
}

// fakeIdentities / fakeTokens satisfy the coding-dispatch ports for the
// ClusterWorkflow path (proxy unset).
type fakeIdentities struct{}

func (fakeIdentities) IdentityFor(context.Context, string) (string, string, string, error) {
	return "aep-bot", "bot@aep.dev", "aep-bot", nil
}

type fakeTokens struct{}

func (fakeTokens) Issue(string, string, string) (string, error) { return "bearer-xyz", nil }

// fakeReeval records Reevaluate calls (the build-success release path).
type fakeReeval struct {
	mu    sync.Mutex
	calls int
}

func (f *fakeReeval) Reevaluate(context.Context) error {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	return nil
}

func (f *fakeReeval) count() int { f.mu.Lock(); defer f.mu.Unlock(); return f.calls }

// fakeRetrier records RetryAuthFailedBuild calls and returns a scripted result.
type fakeRetrier struct {
	newRun string
	err    error
	mu     sync.Mutex
	got    []*models.Execution
}

func (f *fakeRetrier) RetryAuthFailedBuild(_ context.Context, row *models.Execution) (string, error) {
	f.mu.Lock()
	f.got = append(f.got, row)
	f.mu.Unlock()
	return f.newRun, f.err
}

func (f *fakeRetrier) count() int { f.mu.Lock(); defer f.mu.Unlock(); return len(f.got) }

// fakeExecRepo is an in-memory repositories.ExecutionRepository covering the
// verbs the executor + watcher drive (StartWithRun, ListActive, Finish,
// NoteBuildRetry). Unused verbs return zero — they are never reached by these
// tests, and a panic would mask an unexpected call regression, so they no-op
// instead of panicking to keep the fake honest about "not exercised here".
type fakeExecRepo struct {
	mu   sync.Mutex
	rows map[string]*models.Execution
}

func newFakeExecRepo(rows ...*models.Execution) *fakeExecRepo {
	m := map[string]*models.Execution{}
	for _, r := range rows {
		cp := *r
		m[r.ID] = &cp
	}
	return &fakeExecRepo{rows: m}
}

func (f *fakeExecRepo) get(id string) *models.Execution {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r := f.rows[id]; r != nil {
		cp := *r
		return &cp
	}
	return nil
}

func (f *fakeExecRepo) ListActive(context.Context) ([]models.Execution, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []models.Execution
	for _, r := range f.rows {
		if taskmeta.ExecutionStatus(r.Status).IsActive() {
			out = append(out, *r)
		}
	}
	return out, nil
}

func (f *fakeExecRepo) StartWithRun(_ context.Context, id, runName string) (*models.Execution, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r := f.rows[id]
	if r == nil || r.Status != string(taskmeta.ExecQueued) {
		return nil, nil
	}
	r.Status = string(taskmeta.ExecRunning)
	r.RunName = runName
	cp := *r
	return &cp, nil
}

func (f *fakeExecRepo) Finish(_ context.Context, id, status, reason string) (*models.Execution, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r := f.rows[id]
	if r == nil || !taskmeta.ExecutionStatus(r.Status).IsActive() {
		return nil, nil
	}
	r.Status = status
	r.Reason = reason
	cp := *r
	return &cp, nil
}

func (f *fakeExecRepo) NoteBuildRetry(_ context.Context, id, runName, reason string) (*models.Execution, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r := f.rows[id]
	if r == nil || r.Status != string(taskmeta.ExecRunning) {
		return nil, nil
	}
	r.RunName = runName
	r.Reason = reason
	cp := *r
	return &cp, nil
}

// --- unexercised verbs (present to satisfy the interface) -------------------

func (f *fakeExecRepo) TryAdmit(context.Context, *models.Execution) (bool, *models.Execution, error) {
	return false, nil, nil
}
func (f *fakeExecRepo) GetByIDScoped(context.Context, string, string) (*models.Execution, error) {
	return nil, nil
}
func (f *fakeExecRepo) LatestPerKind(context.Context, string, int) (map[string]*models.Execution, error) {
	return nil, nil
}
func (f *fakeExecRepo) LatestPerKindScoped(context.Context, string, string, int) (map[string]*models.Execution, error) {
	return nil, nil
}
func (f *fakeExecRepo) LatestPerKindForRepo(context.Context, string) (map[int]map[string]*models.Execution, error) {
	return nil, nil
}
func (f *fakeExecRepo) LatestPerKindForRepoScoped(context.Context, string, string) (map[int]map[string]*models.Execution, error) {
	return nil, nil
}
func (f *fakeExecRepo) ListByIssue(context.Context, string, int) ([]models.Execution, error) {
	return nil, nil
}
func (f *fakeExecRepo) ListByIssueScoped(context.Context, string, string, int) ([]models.Execution, error) {
	return nil, nil
}
func (f *fakeExecRepo) DeleteByProject(context.Context, string, string) error { return nil }
