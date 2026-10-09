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

// cycle_archive.go — the observability plane's half of a cycle's log.
//
// Once the agent pod is gone there is no pod log left to read, but the
// observability plane has been indexing that pod's output all along. It is read
// by the cycle's Component UID: by COMPONENT scope while the Component's
// release binding resolves, and by PROJECT scope once the Component is deleted
// (the component scope resolves the name through the control plane, and a
// deleted name no longer resolves; the project scope needs only the Project and
// Environment). The UID filter keeps another Component of the project, or a
// later Component reusing the name, out of the cycle's log.
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
	"log/slog"
	"strings"
	"time"

	"github.com/wso2/aep/aep-api/internal/clients/observability"
	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
)

// ErrArchiveUnavailable means the observability plane could not answer — not
// configured, or erroring. Distinct from ErrComponentGone, which means the
// answer is gone for good.
var ErrArchiveUnavailable = errors.New("codingagent: log archive unavailable")

// ObserverArchive reads a cycle's log from the observability plane as text,
// for the v1 surfaces.
type ObserverArchive struct {
	obs     cycleLogQuerier
	runtime openchoreo.RuntimeClient
}

// NewObserverArchive wires the archive. obs may be nil (no OBSERVER_URL): every
// read then reports ErrArchiveUnavailable.
func NewObserverArchive(obs cycleLogQuerier, runtime openchoreo.RuntimeClient) *ObserverArchive {
	return &ObserverArchive{obs: obs, runtime: runtime}
}

// CycleArchive returns the cycle's archived log as timestamped text.
func (a *ObserverArchive) CycleArchive(ctx context.Context, scope ArchiveScope) (string, error) {
	if a == nil {
		return "", fmt.Errorf("%w: no observability plane configured", ErrArchiveUnavailable)
	}
	componentExists := true
	if a.runtime != nil && scope.Environment != "" {
		_, err := a.runtime.ReleaseBindingName(ctx, scope.OrgName, scope.ProjectName, scope.ComponentName, scope.Environment)
		if errors.Is(err, openchoreo.ErrNotFound) {
			componentExists = false
		}
	}
	lines, err := readCycleObserver(ctx, a.obs, scope, componentExists)
	if err != nil {
		return "", err
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

// readCycleObserver is the one observer read of a cycle's lines, for the feed
// and for the v1 archive alike: component scope while the Component exists,
// project scope after, always filtered on the cycle's Component UID. It logs
// `observer.read` with the scope and the read's size.
func readCycleObserver(ctx context.Context, obs cycleLogQuerier, scope ArchiveScope, componentExists bool) ([]observability.LogLine, error) {
	if obs == nil {
		return nil, fmt.Errorf("%w: no observability plane configured", ErrArchiveUnavailable)
	}
	if scope.Environment == "" {
		return nil, fmt.Errorf("%w: no environment for %s", ErrArchiveUnavailable, scope.ComponentName)
	}
	if scope.ComponentUID == "" {
		// No UID to filter on: the cycle predates UID capture, and an
		// unfiltered read would hand it every Component's lines.
		return nil, fmt.Errorf("%w: %s has no recorded component uid", ErrComponentGone, scope.ComponentName)
	}
	q := observability.CycleLogQuery{
		Namespace:    scope.OrgName,
		Project:      scope.ProjectName,
		ComponentUID: scope.ComponentUID,
		Environment:  scope.Environment,
		From:         scope.From,
		To:           scope.To,
	}
	scopeName := "project"
	if componentExists {
		q.Component = openchoreo.ScopedComponentName(scope.ProjectName, scope.ComponentName)
		scopeName = "component"
	}
	lines, stats, err := obs.QueryCycleLogs(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrArchiveUnavailable, err)
	}
	// linesMissing says the read knows it could not return every line in the
	// window (the client warns with the detail); interior losses also show on
	// the feed as gap notices.
	slog.InfoContext(ctx, "observer.read", "cycle", scope.CycleID, "componentUid", scope.ComponentUID,
		"scope", scopeName, "lines", len(lines), "pages", stats.Pages, "linesMissing", stats.LinesMissing)
	return lines, nil
}
