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

package ops_test

import (
	"testing"

	"github.com/wso2/aep/aep-api/internal/ops"
)

func TestClassifyActions(t *testing.T) {
	tests := []struct {
		name     string
		statuses []*string
		want     string
	}{
		{name: "no actions", statuses: nil, want: ops.ClassificationNone},
		{name: "all revised", statuses: ptrs("revised", "revised"), want: ops.ClassificationConfigLevel},
		{name: "all applied", statuses: ptrs("applied"), want: ops.ClassificationConfigLevel},
		{name: "all dismissed", statuses: ptrs("dismissed"), want: ops.ClassificationConfigLevel},
		{name: "suggested is code work", statuses: ptrs("suggested"), want: ops.ClassificationCodeLevel},
		{name: "missing action remains pending", statuses: []*string{nil}, want: ops.ClassificationCodeLevel},
		{name: "unknown action remains pending", statuses: ptrs("unrecognized"), want: ops.ClassificationCodeLevel},
		{name: "config plus pending is mixed", statuses: ptrs("revised", "suggested"), want: ops.ClassificationMixed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ops.ClassifyActions(tt.statuses); got != tt.want {
				t.Fatalf("ClassifyActions(%v) = %q, want %q", tt.statuses, got, tt.want)
			}
		})
	}
}

func TestAdoptableClassification(t *testing.T) {
	tests := []struct {
		name           string
		classification string
		want           bool
	}{
		{name: "config-level alone is not adoptable", classification: ops.ClassificationConfigLevel, want: false},
		{name: "code-level is adoptable", classification: ops.ClassificationCodeLevel, want: true},
		{name: "mixed is adoptable", classification: ops.ClassificationMixed, want: true},
		{name: "none is adoptable", classification: ops.ClassificationNone, want: true},
		{name: "unknown classification is adoptable", classification: "unknown", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ops.AdoptableClassification(tt.classification); got != tt.want {
				t.Fatalf("AdoptableClassification(%q) = %t, want %t", tt.classification, got, tt.want)
			}
		})
	}
}

func ptrs(values ...string) []*string {
	statuses := make([]*string, 0, len(values))
	for _, value := range values {
		value := value
		statuses = append(statuses, &value)
	}
	return statuses
}
