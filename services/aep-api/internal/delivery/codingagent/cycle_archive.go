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

// cycle_archive.go — the POST-MORTEM half of a cycle's log.
//
// Once the agent pod is reaped there is no pod log left to read, but the
// observability plane has been indexing that pod's output all along, keyed on
// the COMPONENT. So the archive is readable for exactly as long as the
// Component is retained — which is why retention deletes Components lazily
// rather than the moment a cycle ends.
//
// Two things this is not. It is not a system of record: the observability
// plane's retention is the dataplane's, and nothing here writes anything back.
// And it is not optional-by-silence: a deployment without an observability
// plane must SAY the logs are unavailable, never show an empty stream that
// reads like an agent that said nothing.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/wso2/aep/aep-api/internal/clients/observability"
	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
)

// ErrArchiveUnavailable means the observability plane could not answer — not
// configured, or erroring. Distinct from ErrComponentGone, which means the
// answer is gone for good.
var ErrArchiveUnavailable = errors.New("codingagent: log archive unavailable")

// ObserverArchive reads a finished cycle's log from the observability plane.
type ObserverArchive struct {
	obs     observability.Client
	runtime openchoreo.RuntimeClient
}

// NewObserverArchive wires the archive. obs may be nil (no OBSERVER_URL): every
// read then reports ErrArchiveUnavailable.
func NewObserverArchive(obs observability.Client, runtime openchoreo.RuntimeClient) *ObserverArchive {
	return &ObserverArchive{obs: obs, runtime: runtime}
}

// CycleArchive returns the cycle's archived log as timestamped text.
func (a *ObserverArchive) CycleArchive(ctx context.Context, scope ArchiveScope) (string, error) {
	if a == nil || a.obs == nil {
		return "", fmt.Errorf("%w: no observability plane configured", ErrArchiveUnavailable)
	}
	// The index dies with the component, so check the component first: querying
	// a deleted one returns an empty page that is indistinguishable from a
	// silent agent, and those are opposite things to tell a user.
	if a.runtime != nil {
		_, err := a.runtime.ReleaseBindingName(ctx, scope.OrgName, scope.ProjectName, scope.ComponentName, openchoreo.DevEnvironmentName)
		if errors.Is(err, openchoreo.ErrNotFound) {
			return "", fmt.Errorf("%w: %s", ErrComponentGone, scope.ComponentName)
		}
	}
	lines, err := a.obs.QueryComponentLogs(ctx, observability.ComponentLogQuery{
		Namespace:   scope.OrgName,
		Project:     scope.ProjectName,
		Component:   openchoreo.ScopedComponentName(scope.ProjectName, scope.ComponentName),
		Environment: openchoreo.DevEnvironmentName,
		From:        scope.From,
		To:          scope.To,
	})
	if err != nil {
		return "", fmt.Errorf("%w: %s", ErrArchiveUnavailable, err)
	}
	var b strings.Builder
	for i := range lines {
		if !lines[i].Timestamp.IsZero() {
			b.WriteString(lines[i].Timestamp.UTC().Format(time.RFC3339Nano))
			b.WriteByte(' ')
		}
		b.WriteString(lines[i].Log)
		b.WriteByte('\n')
	}
	return b.String(), nil
}
