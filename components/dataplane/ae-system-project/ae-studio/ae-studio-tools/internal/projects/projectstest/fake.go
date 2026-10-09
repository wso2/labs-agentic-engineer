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

// Package projectstest is the in-memory projects.Resolver for tests of
// callers that resolve projects.
package projectstest

import (
	"context"
	"sync"

	"github.com/wso2/aep/ae-studio-tools/internal/projects"
)

// Fake resolves from an in-memory map. A name not in it is
// projects.ErrUnknown; the skills repository is the one set with SetSkills
// (projects.ErrUnavailable until then); an error set with SetErr is returned
// for every call (projects.ErrUnavailable simulates aep-api down). Safe for
// concurrent use.
type Fake struct {
	mu     sync.Mutex
	repos  map[string]projects.Repository
	skills *projects.Repository
	err    error
	calls  int
}

var _ projects.Resolver = (*Fake)(nil)

// NewFake returns a Fake that knows repos (copied).
func NewFake(repos map[string]projects.Repository) *Fake {
	f := &Fake{repos: make(map[string]projects.Repository, len(repos))}
	for k, v := range repos {
		f.repos[k] = v
	}
	return f
}

// Set adds or replaces one project.
func (f *Fake) Set(project string, repo projects.Repository) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.repos[project] = repo
}

// SetSkills sets the org's skills repository.
func (f *Fake) SetSkills(repo projects.Repository) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.skills = &repo
}

// SetErr makes every later call return err; nil restores map lookups.
func (f *Fake) SetErr(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

// CallCount is how many times Resolve or ResolveSkills was called.
func (f *Fake) CallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// Resolve implements projects.Resolver.
func (f *Fake) Resolve(_ context.Context, project string) (projects.Repository, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return projects.Repository{}, f.err
	}
	repo, ok := f.repos[project]
	if !ok {
		return projects.Repository{}, projects.ErrUnknown
	}
	return repo, nil
}

// ResolveSkills implements projects.Resolver.
func (f *Fake) ResolveSkills(context.Context) (projects.Repository, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return projects.Repository{}, f.err
	}
	if f.skills == nil {
		return projects.Repository{}, projects.ErrUnavailable
	}
	return *f.skills, nil
}
