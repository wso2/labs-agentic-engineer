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

package spec

import (
	"context"
	"reflect"
	"testing"
)

// ListSpecVersionTags (#117): the spec is versioned as ONE incrementing
// `v<N>` sequence over the whole specs/ tree; legacy `v<N>-<M>` design tags
// are not part of it. specDirty = the specs/ tree at HEAD differs from the
// specs/ tree at the latest tag.

func seedSpec() map[string]string {
	return map[string]string{
		"specs/requirements/prd.md":    "# Reqs\n",
		"specs/design/domain-model.md": "# Design\n",
		"src/main.go":                  "package main\n",
	}
}

func TestListSpecVersionTags_NoTags(t *testing.T) {
	t.Parallel()
	r := newRig(t, seedSpec())

	got, err := r.svc.ListSpecVersionTags(context.Background(), r.org, r.proj)
	if err != nil {
		t.Fatalf("ListSpecVersionTags: %v", err)
	}
	if len(got.Tags) != 0 || got.Latest != "" || got.SpecDirty {
		t.Fatalf("want empty clean TagList, got %+v", got)
	}
}

func TestListSpecVersionTags_LatestCleanAtTag(t *testing.T) {
	t.Parallel()
	r := newRig(t, seedSpec())
	r.remote.Tag(t, "v1", "spec v1")

	got, err := r.svc.ListSpecVersionTags(context.Background(), r.org, r.proj)
	if err != nil {
		t.Fatalf("ListSpecVersionTags: %v", err)
	}
	if !reflect.DeepEqual(got.Tags, []string{"v1"}) || got.Latest != "v1" {
		t.Fatalf("want tags [v1] latest v1, got %+v", got)
	}
	if got.SpecDirty {
		t.Fatalf("HEAD == v1: want clean, got dirty")
	}
}

func TestListSpecVersionTags_DirtyWhenSpecsMovedAfterTag(t *testing.T) {
	t.Parallel()
	r := newRig(t, seedSpec())
	r.remote.Tag(t, "v1", "spec v1")
	r.remote.Seed(t, map[string]string{
		"specs/requirements/prd.md": "# Reqs — edited after v1\n",
	}, "post-tag spec edit")

	got, err := r.svc.ListSpecVersionTags(context.Background(), r.org, r.proj)
	if err != nil {
		t.Fatalf("ListSpecVersionTags: %v", err)
	}
	if !got.SpecDirty {
		t.Fatalf("specs/ changed after v1: want dirty, got %+v", got)
	}
}

func TestListSpecVersionTags_NonSpecCommitStaysClean(t *testing.T) {
	t.Parallel()
	r := newRig(t, seedSpec())
	r.remote.Tag(t, "v1", "spec v1")
	r.remote.Seed(t, map[string]string{
		"src/main.go": "package main // agents wrote code\n",
	}, "post-tag code commit")

	got, err := r.svc.ListSpecVersionTags(context.Background(), r.org, r.proj)
	if err != nil {
		t.Fatalf("ListSpecVersionTags: %v", err)
	}
	if got.SpecDirty {
		t.Fatalf("only src/ changed after v1: want clean, got %+v", got)
	}
}

func TestListSpecVersionTags_SequenceAndLegacyExclusion(t *testing.T) {
	t.Parallel()
	r := newRig(t, seedSpec())
	r.remote.Tag(t, "v1", "spec v1")
	r.remote.Tag(t, "v1-1", "legacy design revision")
	r.remote.Seed(t, map[string]string{
		"specs/design/domain-model.md": "# Design r2\n",
	}, "spec change")
	r.remote.Tag(t, "v2", "spec v2")

	got, err := r.svc.ListSpecVersionTags(context.Background(), r.org, r.proj)
	if err != nil {
		t.Fatalf("ListSpecVersionTags: %v", err)
	}
	if !reflect.DeepEqual(got.Tags, []string{"v2", "v1"}) {
		t.Fatalf("want tags [v2 v1] (newest first, v1-1 excluded), got %+v", got.Tags)
	}
	if got.Latest != "v2" {
		t.Fatalf("want latest v2, got %q", got.Latest)
	}
	if got.SpecDirty {
		t.Fatalf("HEAD == v2: want clean, got dirty")
	}
}
