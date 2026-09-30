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
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// What `aectl sre install` needs to know about the cluster it lands on: the
// observability plane already there (if any), and the SRE agent Deployment
// that plane's chart line renders.

// obsPlaneChart is the chart name of every observability plane release.
const obsPlaneChart = "openchoreo-observability-plane"

// obsPlaneRelease is an installed observability plane Helm release.
type obsPlaneRelease struct {
	Name    string // release name, e.g. "openchoreo-observability-plane"
	Version string // chart version, e.g. "1.2.5"
}

// findObsPlaneRelease picks the observability plane release out of
// `helm list -o json` output. found is false when none is installed.
func findObsPlaneRelease(helmListJSON []byte) (rel obsPlaneRelease, found bool, err error) {
	var releases []struct {
		Name  string `json:"name"`
		Chart string `json:"chart"`
	}
	if err := json.Unmarshal(helmListJSON, &releases); err != nil {
		return obsPlaneRelease{}, false, fmt.Errorf("parse helm list output: %w", err)
	}
	for _, r := range releases {
		// helm reports "<chart>-<version>"; versions may carry their own
		// dashes (1.0.1-hotfix.1), so strip the known chart prefix.
		if v, ok := strings.CutPrefix(r.Chart, obsPlaneChart+"-"); ok && v != "" {
			return obsPlaneRelease{Name: r.Name, Version: v}, true, nil
		}
	}
	return obsPlaneRelease{}, false, nil
}

// installedObsPlane returns the observability plane release in ns, if any.
func installedObsPlane(ctx context.Context, ns string) (obsPlaneRelease, bool, error) {
	out, err := exec.CommandContext(ctx, "helm", "list", "-n", ns, "-o", "json").Output()
	if err != nil {
		return obsPlaneRelease{}, false, fmt.Errorf("helm list -n %s: %w", ns, err)
	}
	return findObsPlaneRelease(out)
}

// sreAgentComponents are the app.kubernetes.io/component labels the SRE agent
// Deployment carries: "sre-agent" from chart 1.2.0, "ai-rca-agent" before.
// The chart names both the Deployment and its container after it.
var sreAgentComponents = []string{"sre-agent", "ai-rca-agent"}

// findSREAgentDeployment returns the name of the SRE agent Deployment the
// observability plane chart rendered in ns.
func findSREAgentDeployment(ctx context.Context, client kubernetes.Interface, ns string) (string, error) {
	selector := fmt.Sprintf("app.kubernetes.io/name=%s,app.kubernetes.io/component in (%s)",
		obsPlaneChart, strings.Join(sreAgentComponents, ","))
	list, err := client.AppsV1().Deployments(ns).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return "", fmt.Errorf("list SRE agent deployments in %s: %w", ns, err)
	}
	switch len(list.Items) {
	case 1:
		return list.Items[0].Name, nil
	case 0:
		return "", fmt.Errorf("no SRE agent deployment (component %s) in %s — is rca.enabled set on the observability plane?",
			strings.Join(sreAgentComponents, " or "), ns)
	default:
		return "", fmt.Errorf("%d SRE agent deployments in %s; expected one", len(list.Items), ns)
	}
}
