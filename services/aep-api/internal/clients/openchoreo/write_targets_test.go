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

package openchoreo

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/gen"
)

type fakeProjects struct {
	byName map[string]*gen.Project
	err    error
	calls  int
}

func (f *fakeProjects) GetProject(_ context.Context, _, name string) (*gen.Project, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	p, ok := f.byName[name]
	if !ok {
		return nil, fmt.Errorf("get project %q: %w", name, ErrNotFound)
	}
	return p, nil
}

type fakePipelines struct {
	byName map[string]*deploymentPipeline
	err    error
	calls  int
}

func (f *fakePipelines) getPipeline(_ context.Context, _, name string) (*deploymentPipeline, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	p, ok := f.byName[name]
	if !ok {
		return nil, fmt.Errorf("get deployment pipeline %q: %w", name, ErrNotFound)
	}
	return p, nil
}

func (f *fakePipelines) ListPipelineNames(_ context.Context, _ string) ([]string, error) {
	f.calls++
	names := make([]string, 0, len(f.byName))
	for n := range f.byName {
		names = append(names, n)
	}
	sort.Strings(names)
	return names, f.err
}

func k3dChain() *deploymentPipeline {
	return pipelineOf([2]any{"development", []string{"staging"}}, [2]any{"staging", []string{"production"}})
}

func testPipelines() *fakePipelines {
	return &fakePipelines{byName: map[string]*deploymentPipeline{
		"default":    k3dChain(),
		"pipeline-b": pipelineOf([2]any{"dev-b", []string{"prod"}}),
		"cyclic":     pipelineOf([2]any{"a", []string{"b"}}, [2]any{"b", []string{"a"}}),
		"self":       pipelineOf([2]any{"default", []string{"default"}}),
		"empty":      {},
		"long":       pipelineOf([2]any{"integration-1", nil}),
	}}
}

func proj(name, pipeline string) *gen.Project {
	return &gen.Project{Name: name, DeploymentPipeline: pipeline}
}

func TestWriteTargets_Resolve(t *testing.T) {
	projects := map[string]*gen.Project{
		"on-default": proj("on-default", "default"),
		"on-b":       proj("on-b", "pipeline-b"),
		"no-ref":     proj("no-ref", ""),
		"gone-ref":   proj("gone-ref", "gone"),
		"on-cyclic":  proj("on-cyclic", "cyclic"),
		"on-self":    proj("on-self", "self"),
		"on-empty":   proj("on-empty", "empty"),
		"on-long":    proj("on-long", "long"),
	}
	cases := []struct {
		name         string
		project      string
		want         string
		wantPipeline string
		cause        error
	}{
		{"default pipeline chain", "on-default", "development", "", nil},
		{"per-project pipeline, not per-org", "on-b", "dev-b", "", nil},
		{"no pipeline ref", "no-ref", "", "", ErrPipelineRefMissing},
		{"pipeline 404", "gone-ref", "", "gone", ErrNotFound},
		{"cyclic", "on-cyclic", "", "cyclic", ErrPipelineCyclic},
		{"self edge", "on-self", "", "self", ErrPipelineCyclic},
		{"empty", "on-empty", "", "empty", ErrPipelineEmpty},
		{"root too long", "on-long", "", "long", ErrWriteTargetTooLong},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wt := &writeTargets{projects: &fakeProjects{byName: projects}, pipelines: testPipelines()}
			got, err := wt.Resolve(context.Background(), "acme", tc.project)
			if tc.cause == nil {
				if err != nil || got != tc.want {
					t.Fatalf("Resolve = (%q, %v), want (%q, nil)", got, err, tc.want)
				}
				return
			}
			var nwt *ErrNoWriteTarget
			if !errors.As(err, &nwt) {
				t.Fatalf("err = %v, want *ErrNoWriteTarget", err)
			}
			if !errors.Is(err, tc.cause) {
				t.Fatalf("err = %v, want cause %v", err, tc.cause)
			}
			if nwt.Org != "acme" || nwt.Project != tc.project || nwt.Pipeline != tc.wantPipeline {
				t.Fatalf("ErrNoWriteTarget = %+v", nwt)
			}
			if got != "" {
				t.Fatalf("got %q with an error", got)
			}
		})
	}

	t.Run("project 404 is not a write-target fact", func(t *testing.T) {
		wt := &writeTargets{projects: &fakeProjects{byName: projects}, pipelines: testPipelines()}
		_, err := wt.Resolve(context.Background(), "acme", "missing")
		var nwt *ErrNoWriteTarget
		if errors.As(err, &nwt) || !errors.Is(err, ErrNotFound) {
			t.Fatalf("err = %v, want plain ErrNotFound", err)
		}
	})

	t.Run("transient pipeline read propagates unchanged", func(t *testing.T) {
		transient := errors.New("503")
		pl := testPipelines()
		pl.err = transient
		wt := &writeTargets{projects: &fakeProjects{byName: projects}, pipelines: pl}
		_, err := wt.Resolve(context.Background(), "acme", "on-default")
		var nwt *ErrNoWriteTarget
		if errors.As(err, &nwt) || !errors.Is(err, transient) {
			t.Fatalf("err = %v, want the transient error, not ErrNoWriteTarget", err)
		}
	})

	t.Run("empty arguments make no OC call", func(t *testing.T) {
		fp, fl := &fakeProjects{byName: projects}, testPipelines()
		wt := &writeTargets{projects: fp, pipelines: fl}
		if _, err := wt.Resolve(context.Background(), "", "on-default"); err == nil {
			t.Fatal("empty org accepted")
		}
		if _, err := wt.Resolve(context.Background(), "acme", ""); err == nil {
			t.Fatal("empty project accepted")
		}
		if fp.calls+fl.calls != 0 {
			t.Fatalf("OC called %d times", fp.calls+fl.calls)
		}
	})
}

func TestWriteTargets_OrgDefaultRoot(t *testing.T) {
	newWT := func(pipelines map[string]*deploymentPipeline) *writeTargets {
		return &writeTargets{projects: &fakeProjects{}, pipelines: &fakePipelines{byName: pipelines}}
	}
	t.Run("default among several", func(t *testing.T) {
		got, err := newWT(testPipelines().byName).OrgDefaultRoot(context.Background(), "acme")
		if err != nil || got != "development" {
			t.Fatalf("got (%q, %v)", got, err)
		}
	})
	t.Run("sole non-default pipeline", func(t *testing.T) {
		got, err := newWT(map[string]*deploymentPipeline{"only": pipelineOf([2]any{"dev-b", nil})}).OrgDefaultRoot(context.Background(), "acme")
		if err != nil || got != "dev-b" {
			t.Fatalf("got (%q, %v)", got, err)
		}
	})
	t.Run("two pipelines, none default", func(t *testing.T) {
		_, err := newWT(map[string]*deploymentPipeline{
			"x": pipelineOf([2]any{"a", nil}), "y": pipelineOf([2]any{"b", nil}),
		}).OrgDefaultRoot(context.Background(), "acme")
		var nwt *ErrNoWriteTarget
		if !errors.As(err, &nwt) {
			t.Fatalf("err = %v, want *ErrNoWriteTarget", err)
		}
		if nwt.Org != "acme" || nwt.Project != "" {
			t.Fatalf("ErrNoWriteTarget = %+v", nwt)
		}
		if msg := nwt.Cause.Error(); !strings.Contains(msg, "x") || !strings.Contains(msg, "y") {
			t.Fatalf("cause %q does not name the candidates", msg)
		}
	})
	t.Run("zero pipelines", func(t *testing.T) {
		_, err := newWT(map[string]*deploymentPipeline{}).OrgDefaultRoot(context.Background(), "acme")
		var nwt *ErrNoWriteTarget
		if !errors.As(err, &nwt) {
			t.Fatalf("err = %v, want *ErrNoWriteTarget", err)
		}
	})
}
