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

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
)

// The binding is looked up in the environment the caller names: the one the
// cycle's Job was bound into.
func TestOCLogSource_BindingIsReadInTheNamedEnvironment(t *testing.T) {
	rt := &fakeRuntime{}

	if _, err := NewOCLogSource(rt).Binding(context.Background(), "acme", "shop", "ca-abc", "dev-b"); err != nil {
		t.Fatalf("Binding: %v", err)
	}
	if len(rt.bindingEnvs) != 1 || rt.bindingEnvs[0] != "dev-b" {
		t.Fatalf("binding read in %v, want [dev-b]", rt.bindingEnvs)
	}
}

// No environment is a caller bug, and is refused rather than read as "".
func TestOCLogSource_BindingRefusesAnEmptyEnvironment(t *testing.T) {
	rt := &fakeRuntime{}

	if _, err := NewOCLogSource(rt).Binding(context.Background(), "acme", "shop", "ca-abc", ""); err == nil {
		t.Fatal("Binding with no environment succeeded, want a refusal")
	}
	if rt.bindingCalls != 0 {
		t.Fatalf("binding calls = %d, want none", rt.bindingCalls)
	}
}

func TestOCLogSource_TailReturnsPodTextAndPhase(t *testing.T) {
	rt := &fakeRuntime{
		pod: openchoreo.RuntimePod{Found: true, Name: "p1", Phase: "Running"},
		logs: []openchoreo.PodLogLine{
			{Timestamp: time.Date(2026, 8, 6, 10, 0, 1, 0, time.UTC), Log: "first"},
			{Timestamp: time.Date(2026, 8, 6, 10, 0, 2, 0, time.UTC), Log: "second"},
		},
	}

	got, err := NewOCLogSource(rt).Tail(context.Background(), "acme", "shop", "ca-abc", "development", logPageBytes)
	if err != nil {
		t.Fatalf("Tail: %v", err)
	}
	if got.Pod.Phase != "Running" {
		t.Fatalf("Pod = %+v", got.Pod)
	}
	// The platform's timestamps are re-emitted in the k8s `timestamps=true`
	// shape the progress parser already splits, so one parser serves both eras.
	if !strings.HasPrefix(got.Text, "2026-08-06T10:00:01Z first\n") {
		t.Fatalf("unexpected text %q", got.Text)
	}
	if !strings.Contains(got.Text, "2026-08-06T10:00:02Z second") {
		t.Fatalf("unexpected text %q", got.Text)
	}
}

func TestOCLogSource_TailKeepsTheNewestBytes(t *testing.T) {
	rt := &fakeRuntime{
		pod: openchoreo.RuntimePod{Found: true, Name: "p1", Phase: "Running"},
		logs: []openchoreo.PodLogLine{
			{Timestamp: time.Date(2026, 8, 6, 10, 0, 1, 0, time.UTC), Log: strings.Repeat("a", 500)},
			{Timestamp: time.Date(2026, 8, 6, 10, 0, 2, 0, time.UTC), Log: "newest"},
		},
	}

	got, err := NewOCLogSource(rt).Tail(context.Background(), "acme", "shop", "ca-abc", "development", 64)
	if err != nil {
		t.Fatalf("Tail: %v", err)
	}
	if len(got.Text) > 64 {
		t.Fatalf("tail = %d bytes, want <= 64", len(got.Text))
	}
	if !strings.Contains(got.Text, "newest") {
		t.Fatalf("the tail must keep the newest lines, got %q", got.Text)
	}
}

// A deleted Component is a fact the reader turns into an "unavailable" empty
// state, so it has to be distinguishable from a transport error.
func TestOCLogSource_MissingComponentIsComponentGone(t *testing.T) {
	rt := &fakeRuntime{bindingErr: fmt.Errorf("%w: gone", openchoreo.ErrNotFound)}

	_, err := NewOCLogSource(rt).Tail(context.Background(), "acme", "shop", "ca-abc", "development", logPageBytes)
	if !errors.Is(err, ErrComponentGone) {
		t.Fatalf("err = %v, want ErrComponentGone", err)
	}
}

func TestOCLogSource_UnscheduledPodIsEmptyNotAnError(t *testing.T) {
	rt := &fakeRuntime{pod: openchoreo.RuntimePod{}}

	got, err := NewOCLogSource(rt).Tail(context.Background(), "acme", "shop", "ca-abc", "development", logPageBytes)
	if err != nil {
		t.Fatalf("Tail: %v", err)
	}
	if got.Pod.Found || got.Text != "" {
		t.Fatalf("unexpected tail: %+v", got)
	}
}

// TestOCLogSource_TailStillAsksForTheWholeLog pins the viewer's semantics: Tail
// asks for everything the platform holds and cuts BYTES off the end of it,
// because a viewer that loses old bytes gets them back on the next poll.
func TestOCLogSource_TailStillAsksForTheWholeLog(t *testing.T) {
	rt := &fakeRuntime{
		pod:  openchoreo.RuntimePod{Found: true, Name: "p1", Phase: "Running"},
		logs: []openchoreo.PodLogLine{{Timestamp: time.Date(2026, 9, 8, 9, 29, 44, 0, time.UTC), Log: "line"}},
	}
	if _, err := NewOCLogSource(rt).Tail(context.Background(), "acme", "shop", "ca-abc", "development", logPageBytes); err != nil {
		t.Fatalf("Tail: %v", err)
	}
	if rt.logSince != 0 {
		t.Errorf("sinceSeconds = %d, want 0 (the whole log)", rt.logSince)
	}
}
