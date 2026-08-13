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
	got   observability.ComponentLogQuery
	calls int
}

func (f *fakeObserver) GetBuildLogs(context.Context, string, string, string, string, time.Time) (*gen.BuildLogs, error) {
	panic("fakeObserver: GetBuildLogs not expected")
}

func (f *fakeObserver) QueryComponentLogs(_ context.Context, q observability.ComponentLogQuery) ([]observability.LogLine, error) {
	f.calls++
	f.got = q
	return f.lines, f.err
}

func TestCycleArchive_QueriesTheComponentScopeAndRendersTimestampedText(t *testing.T) {
	obs := &fakeObserver{lines: []observability.LogLine{
		{Timestamp: time.Date(2026, 8, 6, 10, 0, 1, 0, time.UTC), Log: "first"},
		{Timestamp: time.Date(2026, 8, 6, 10, 0, 2, 0, time.UTC), Log: "second"},
	}}
	rt := &fakeRuntime{}

	from := time.Date(2026, 8, 6, 9, 55, 0, 0, time.UTC)
	text, err := NewObserverArchive(obs, rt).CycleArchive(context.Background(), ArchiveScope{
		OrgName: "acme", ProjectName: "shop", ComponentName: "ca-abc",
		From: from, To: from.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("CycleArchive: %v", err)
	}
	if obs.got.Component != openchoreo.ScopedComponentName("shop", "ca-abc") {
		t.Fatalf("component = %q, want the scoped name", obs.got.Component)
	}
	if obs.got.Namespace != "acme" || obs.got.Environment != openchoreo.DevEnvironmentName {
		t.Fatalf("unexpected scope: %+v", obs.got)
	}
	if !strings.HasPrefix(text, "2026-08-06T10:00:01Z first\n") {
		t.Fatalf("unexpected text %q", text)
	}
}

// The observer indexes on the component CR. Once retention deletes it the
// archive is gone too, so a deleted component is answered without a query at
// all — asking would only produce a confusing empty result.
func TestCycleArchive_DeletedComponentIsComponentGone(t *testing.T) {
	obs := &fakeObserver{}
	rt := &fakeRuntime{bindingErr: fmt.Errorf("%w: gone", openchoreo.ErrNotFound)}

	_, err := NewObserverArchive(obs, rt).CycleArchive(context.Background(), ArchiveScope{
		OrgName: "acme", ProjectName: "shop", ComponentName: "ca-abc",
	})
	if !errors.Is(err, ErrComponentGone) {
		t.Fatalf("err = %v, want ErrComponentGone", err)
	}
	if obs.calls != 0 {
		t.Fatalf("a deleted component must not be queried, got %d calls", obs.calls)
	}
}

func TestCycleArchive_NoObserverIsUnavailable(t *testing.T) {
	_, err := NewObserverArchive(nil, &fakeRuntime{}).CycleArchive(context.Background(), ArchiveScope{
		OrgName: "acme", ProjectName: "shop", ComponentName: "ca-abc",
	})
	if !errors.Is(err, ErrArchiveUnavailable) {
		t.Fatalf("err = %v, want ErrArchiveUnavailable", err)
	}
}

func TestCycleArchive_ObserverFailureIsUnavailable(t *testing.T) {
	obs := &fakeObserver{err: errors.New("observer: 503")}

	_, err := NewObserverArchive(obs, &fakeRuntime{}).CycleArchive(context.Background(), ArchiveScope{
		OrgName: "acme", ProjectName: "shop", ComponentName: "ca-abc",
	})
	if !errors.Is(err, ErrArchiveUnavailable) {
		t.Fatalf("err = %v, want ErrArchiveUnavailable", err)
	}
}

func TestCycleArchive_EmptyResultIsEmptyTextNotAnError(t *testing.T) {
	obs := &fakeObserver{}

	text, err := NewObserverArchive(obs, &fakeRuntime{}).CycleArchive(context.Background(), ArchiveScope{
		OrgName: "acme", ProjectName: "shop", ComponentName: "ca-abc",
	})
	if err != nil {
		t.Fatalf("CycleArchive: %v", err)
	}
	if text != "" {
		t.Fatalf("text = %q, want empty", text)
	}
}
