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
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/clients/observability"
	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/gen"
)

type fakeObserver struct {
	lines []observability.LogLine
	err   error
	got   observability.CycleLogQuery
	calls int
}

func (f *fakeObserver) GetBuildLogs(context.Context, string, string, string, string, time.Time) (*gen.BuildLogs, error) {
	panic("fakeObserver: GetBuildLogs not expected")
}

func (f *fakeObserver) QueryCycleLogs(_ context.Context, q observability.CycleLogQuery) ([]observability.LogLine, observability.CycleLogStats, error) {
	f.calls++
	f.got = q
	return f.lines, observability.CycleLogStats{Pages: 1}, f.err
}

func TestCycleArchive_QueriesTheComponentScopeAndRendersTimestampedText(t *testing.T) {
	obs := &fakeObserver{lines: []observability.LogLine{
		{Timestamp: time.Date(2026, 8, 6, 10, 0, 1, 0, time.UTC), Log: "first"},
		{Timestamp: time.Date(2026, 8, 6, 10, 0, 2, 0, time.UTC), Log: "second"},
	}}
	rt := &fakeRuntime{}

	from := time.Date(2026, 8, 6, 9, 55, 0, 0, time.UTC)
	text, err := NewObserverArchive(obs, rt).CycleArchive(context.Background(), ArchiveScope{
		OrgName: "acme", ProjectName: "shop", ComponentName: "ca-abc", ComponentUID: "uid-abc", Environment: "dev-b",
		From: from, To: from.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("CycleArchive: %v", err)
	}
	if obs.got.Component != openchoreo.ScopedComponentName("shop", "ca-abc") {
		t.Fatalf("component = %q, want the scoped name", obs.got.Component)
	}
	if obs.got.ComponentUID != "uid-abc" {
		t.Fatalf("componentUid = %q, want the cycle's recorded UID", obs.got.ComponentUID)
	}
	if !obs.got.From.Equal(from) || !obs.got.To.Equal(from.Add(time.Hour)) {
		t.Fatalf("window = %v..%v, want the scope's", obs.got.From, obs.got.To)
	}
	if obs.got.Namespace != "acme" || obs.got.Environment != "dev-b" {
		t.Fatalf("unexpected scope: %+v", obs.got)
	}
	if len(rt.bindingEnvs) != 1 || rt.bindingEnvs[0] != "dev-b" {
		t.Fatalf("component check read in %v, want [dev-b]", rt.bindingEnvs)
	}
	if !strings.HasPrefix(text, "2026-08-06T10:00:01Z first\n") {
		t.Fatalf("unexpected text %q", text)
	}
}

// A deleted Component no longer resolves for the component scope, but its
// lines are still in the index: the project scope reads them, filtered on the
// cycle's Component UID.
func TestCycleArchive_DeletedComponentReadsTheProjectScope(t *testing.T) {
	obs := &fakeObserver{lines: []observability.LogLine{{Timestamp: time.Date(2026, 8, 6, 10, 0, 1, 0, time.UTC), Log: "kept"}}}
	rt := &fakeRuntime{bindingErr: fmt.Errorf("%w: gone", openchoreo.ErrNotFound)}

	from := time.Date(2026, 8, 6, 9, 55, 0, 0, time.UTC)
	text, err := NewObserverArchive(obs, rt).CycleArchive(context.Background(), ArchiveScope{
		OrgName: "acme", ProjectName: "shop", ComponentName: "ca-abc", ComponentUID: "uid-abc", Environment: "development",
		From: from, To: from.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("CycleArchive: %v", err)
	}
	if obs.got.Component != "" || obs.got.ComponentUID != "uid-abc" || obs.got.Project != "shop" {
		t.Fatalf("query %+v, want the project scope filtered on the UID", obs.got)
	}
	if text != "2026-08-06T10:00:01Z kept\n" {
		t.Fatalf("text %q", text)
	}
}

// A cycle from before UID capture has nothing to filter the index on; an
// unfiltered read would hand it every Component's lines.
func TestCycleArchive_NoComponentUIDIsNotRead(t *testing.T) {
	obs := &fakeObserver{}
	_, err := NewObserverArchive(obs, &fakeRuntime{}).CycleArchive(context.Background(), ArchiveScope{
		OrgName: "acme", ProjectName: "shop", ComponentName: "ca-abc", Environment: "development",
	})
	if !errors.Is(err, ErrComponentGone) || obs.calls != 0 {
		t.Fatalf("err = %v, calls = %d, want ErrComponentGone and no query", err, obs.calls)
	}
}

func TestCycleArchive_NoObserverIsUnavailable(t *testing.T) {
	_, err := NewObserverArchive(nil, &fakeRuntime{}).CycleArchive(context.Background(), ArchiveScope{
		OrgName: "acme", ProjectName: "shop", ComponentName: "ca-abc", Environment: "development",
	})
	if !errors.Is(err, ErrArchiveUnavailable) {
		t.Fatalf("err = %v, want ErrArchiveUnavailable", err)
	}
}

func TestCycleArchive_ObserverFailureIsUnavailable(t *testing.T) {
	obs := &fakeObserver{err: errors.New("observer: 503")}

	_, err := NewObserverArchive(obs, &fakeRuntime{}).CycleArchive(context.Background(), ArchiveScope{
		OrgName: "acme", ProjectName: "shop", ComponentName: "ca-abc", ComponentUID: "uid-abc", Environment: "development",
		From: time.Now().Add(-time.Hour), To: time.Now(),
	})
	if !errors.Is(err, ErrArchiveUnavailable) {
		t.Fatalf("err = %v, want ErrArchiveUnavailable", err)
	}
}

func TestCycleArchive_EmptyResultIsEmptyTextNotAnError(t *testing.T) {
	obs := &fakeObserver{}

	text, err := NewObserverArchive(obs, &fakeRuntime{}).CycleArchive(context.Background(), ArchiveScope{
		OrgName: "acme", ProjectName: "shop", ComponentName: "ca-abc", ComponentUID: "uid-abc", Environment: "development",
		From: time.Now().Add(-time.Hour), To: time.Now(),
	})
	if err != nil {
		t.Fatalf("CycleArchive: %v", err)
	}
	if text != "" {
		t.Fatalf("text = %q, want empty", text)
	}
}
