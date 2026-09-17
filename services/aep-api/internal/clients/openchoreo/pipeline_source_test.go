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
	"errors"
	"strings"
	"testing"
)

type promotionPath = struct {
	SourceEnvironmentRef struct {
		Name string `json:"name"`
	} `json:"sourceEnvironmentRef"`
	TargetEnvironmentRefs []struct {
		Name string `json:"name"`
	} `json:"targetEnvironmentRefs"`
}

func appendPromotionPath(p *deploymentPipeline, src string, targets ...string) {
	var refs []struct {
		Name string `json:"name"`
	}
	for _, n := range targets {
		refs = append(refs, struct {
			Name string `json:"name"`
		}{Name: n})
	}
	path := promotionPath{}
	path.SourceEnvironmentRef.Name = src
	path.TargetEnvironmentRefs = refs
	p.Spec.PromotionPaths = append(p.Spec.PromotionPaths, path)
}

func pipelineWith(source string) *deploymentPipeline {
	p := &deploymentPipeline{}
	appendPromotionPath(p, source)
	return p
}

func pipelineLinear(envs ...string) *deploymentPipeline {
	p := &deploymentPipeline{}
	for i := 0; i < len(envs)-1; i++ {
		appendPromotionPath(p, envs[i], envs[i+1])
	}
	return p
}

func pipelineFanOut(source string, targets ...string) *deploymentPipeline {
	p := &deploymentPipeline{}
	for _, t := range targets {
		appendPromotionPath(p, source, t)
	}
	return p
}

func twoSources(a, b, target string) *deploymentPipeline {
	p := &deploymentPipeline{}
	appendPromotionPath(p, a, target)
	appendPromotionPath(p, b, target)
	return p
}

func pipelineCycle(a, b string) *deploymentPipeline {
	p := &deploymentPipeline{}
	appendPromotionPath(p, a, b)
	appendPromotionPath(p, b, a)
	return p
}

func TestPipelineSourceEnvironment(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		p       *deploymentPipeline
		want    string
		wantErr error
	}{
		{
			name: "k3d single env default",
			p:    pipelineWith("default"),
			want: "default",
		},
		{
			name: "getting-started development staging production",
			p:    pipelineLinear("development", "staging", "production"),
			want: "development",
		},
		{
			name: "fan-out from development",
			p:    pipelineFanOut("development", "staging", "qa"),
			want: "development",
		},
		{
			name:    "empty paths",
			p:       &deploymentPipeline{},
			wantErr: ErrPipelineEmpty,
		},
		{
			name:    "nil pipeline",
			p:       nil,
			wantErr: ErrPipelineEmpty,
		},
		{
			name:    "two sources",
			p:       twoSources("dev", "experimental", "staging"),
			wantErr: ErrPipelineSourceAmbiguous,
		},
		{
			name:    "cycle",
			p:       pipelineCycle("staging", "production"),
			wantErr: ErrPipelineCyclic,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := PipelineSourceEnvironment(tc.p)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				if got != "" {
					t.Fatalf("got %q, want empty on error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestPipelineSourceEnvironment_ErrorNamesThePipeline(t *testing.T) {
	t.Parallel()
	_, err := PipelineSourceEnvironment(&deploymentPipeline{})
	if err == nil || !strings.Contains(err.Error(), "default/default") {
		t.Fatalf("empty error %v, want it to name default/default", err)
	}
}
