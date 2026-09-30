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

package cmd

import (
	"context"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/kubernetes/fake"
)

func TestFindObsPlaneReleaseReadsChartVersion(t *testing.T) {
	out := []byte(`[
		{"name":"observability-logs-opensearch","chart":"observability-logs-opensearch-0.5.3"},
		{"name":"openchoreo-observability-plane","chart":"openchoreo-observability-plane-1.2.5"}
	]`)
	rel, found, err := findObsPlaneRelease(out)
	if err != nil || !found {
		t.Fatalf("found=%v err=%v", found, err)
	}
	if rel.Name != "openchoreo-observability-plane" || rel.Version != "1.2.5" {
		t.Fatalf("got %+v", rel)
	}
}

func TestFindObsPlaneReleaseKeepsPrereleaseVersion(t *testing.T) {
	out := []byte(`[{"name":"observability-plane","chart":"openchoreo-observability-plane-1.0.1-hotfix.1"}]`)
	rel, found, err := findObsPlaneRelease(out)
	if err != nil || !found || rel.Version != "1.0.1-hotfix.1" {
		t.Fatalf("rel=%+v found=%v err=%v", rel, found, err)
	}
}

func TestFindObsPlaneReleaseNoneInstalled(t *testing.T) {
	_, found, err := findObsPlaneRelease([]byte(`[{"name":"x","chart":"observability-logs-opensearch-0.5.3"}]`))
	if err != nil || found {
		t.Fatalf("found=%v err=%v", found, err)
	}
}

func sreDeployment(name, component string) *appsv1.Deployment {
	return &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{
		Name: name, Namespace: "obs",
		Labels: map[string]string{
			"app.kubernetes.io/name":      obsPlaneChart,
			"app.kubernetes.io/component": component,
		},
	}}
}

func TestFindSREAgentDeploymentAcrossChartLines(t *testing.T) {
	for _, name := range sreAgentComponents {
		client := fake.NewSimpleClientset(sreDeployment(name, name), sreDeployment("observer", "observer"))
		got, err := findSREAgentDeployment(context.Background(), client, "obs")
		if err != nil || got != name {
			t.Fatalf("component %s: got %q err=%v", name, got, err)
		}
	}
}

func TestFindSREAgentDeploymentMissingNamesRCAEnabled(t *testing.T) {
	client := fake.NewSimpleClientset(sreDeployment("observer", "observer"))
	_, err := findSREAgentDeployment(context.Background(), client, "obs")
	if err == nil || !strings.Contains(err.Error(), "rca.enabled") {
		t.Fatalf("err = %v", err)
	}
}

func externalSecret(refreshTime, ready string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]interface{}{
		"status": map[string]interface{}{
			"refreshTime": refreshTime,
			"conditions":  []interface{}{map[string]interface{}{"type": "Ready", "status": ready}},
		},
	}}
}

func TestExternalSecretSyncedSince(t *testing.T) {
	since, _ := time.Parse(time.RFC3339, "2026-09-27T11:30:00Z")
	cases := []struct {
		name, refresh, ready string
		want                 bool
	}{
		{"refreshed after", "2026-09-27T11:30:05Z", "True", true},
		{"refreshed same second", "2026-09-27T11:30:00Z", "True", true},
		{"stale refresh", "2026-09-27T11:29:59Z", "True", false},
		{"not ready", "2026-09-27T11:30:05Z", "False", false},
		{"never refreshed", "", "True", false},
	}
	for _, c := range cases {
		if got := externalSecretSyncedSince(externalSecret(c.refresh, c.ready), since); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}
