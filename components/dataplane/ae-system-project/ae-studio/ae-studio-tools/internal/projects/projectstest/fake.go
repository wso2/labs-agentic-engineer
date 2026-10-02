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

// Fake resolves from Repos. Err, when set, is returned for every call (use
// projects.ErrUnavailable to simulate aep-api down); a name not in Repos is
// projects.ErrUnknown. Calls counts Resolve calls. Safe for concurrent use.
type Fake struct {
	mu    sync.Mutex
	Repos map[string]projects.Repository
	Err   error
	Calls int
}

var _ projects.Resolver = (*Fake)(nil)

// Resolve implements projects.Resolver.
func (f *Fake) Resolve(_ context.Context, project string) (projects.Repository, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls++
	if f.Err != nil {
		return projects.Repository{}, f.Err
	}
	repo, ok := f.Repos[project]
	if !ok {
		return projects.Repository{}, projects.ErrUnknown
	}
	return repo, nil
}
