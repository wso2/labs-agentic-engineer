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
	"regexp"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/kubernetes"

	k8s "github.com/wso2/aep/aectl/internal/kubernetes"
)

// What `aectl sre install` needs to know about the cluster it lands on: the
// observability plane already there (if any), the SRE agent Deployment that
// plane's chart line renders, and where the org's Console-saved model key
// lives.

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

// The org's model connection key, as aep-api publishes it when the key is
// saved in the Console: a SecretReference in the org's OpenChoreo namespace
// whose api-key entry points at the key's KV path. Every save mints a new
// reference named <namespace>-default-key-<8 hex> (the org_secrets entity
// "default-key"; a long namespace is trimmed, the entity and suffix never are)
// and retires the previous one once the new one is committed. Reading the
// coordinates from that CR, rather than rebuilding the path, keeps aep-api the
// only place that knows how the path is derived.
//
// The pre-reference names (model-connection-secrets, anthropic-secrets) are
// not read: an org that has them and no default-key reference has not
// re-entered its key since, and those references point at a stale copy.
const orgAnthropicSecretKey = "api-key"

var orgDefaultKeyRefName = regexp.MustCompile(`(^|-)default-key-[0-9a-f]{8}$`)

// currentDefaultKeyRef picks the org's current default-key SecretReference by
// name. More than one exists only for the moment between a save's commit and
// the retirement of the reference it replaced (or when that retirement
// failed), and in both cases the newest is the one aep-api points at.
func currentDefaultKeyRef(refs []unstructured.Unstructured) (*unstructured.Unstructured, bool) {
	var current *unstructured.Unstructured
	for i := range refs {
		if !orgDefaultKeyRefName.MatchString(refs[i].GetName()) {
			continue
		}
		if current == nil {
			current = &refs[i]
			continue
		}
		newest, candidate := current.GetCreationTimestamp(), refs[i].GetCreationTimestamp()
		if newest.Before(&candidate) {
			current = &refs[i]
		}
	}
	return current, current != nil
}

// kvRef is a secret-store remote reference: a KV path and a property in it.
type kvRef struct {
	Key, Property string
}

// orgAnthropicKVRef extracts the api-key remote reference from the org's
// model key SecretReference.
func orgAnthropicKVRef(ref *unstructured.Unstructured) (kvRef, error) {
	data, _, err := unstructured.NestedSlice(ref.Object, "spec", "data")
	if err != nil {
		return kvRef{}, fmt.Errorf("read spec.data of SecretReference %s/%s: %w", ref.GetNamespace(), ref.GetName(), err)
	}
	for _, d := range data {
		entry, ok := d.(map[string]interface{})
		if !ok {
			continue
		}
		if sk, _, _ := unstructured.NestedString(entry, "secretKey"); sk != orgAnthropicSecretKey {
			continue
		}
		key, _, _ := unstructured.NestedString(entry, "remoteRef", "key")
		prop, _, _ := unstructured.NestedString(entry, "remoteRef", "property")
		if key == "" {
			break
		}
		return kvRef{Key: key, Property: prop}, nil
	}
	return kvRef{}, fmt.Errorf("SecretReference %s/%s has no %s entry with a remoteRef.key",
		ref.GetNamespace(), ref.GetName(), orgAnthropicSecretKey)
}

// resolveOrgAnthropicKVRef finds the org's Console-saved model connection key.
// found is false until someone saves the key in the Console.
func resolveOrgAnthropicKVRef(ctx context.Context, applier *k8s.Applier, orgNamespace string) (ref kvRef, found bool, err error) {
	refs, err := applier.List(ctx, "openchoreo.dev/v1alpha1", "SecretReference", orgNamespace)
	if err != nil {
		return kvRef{}, false, err
	}
	obj, ok := currentDefaultKeyRef(refs)
	if !ok {
		return kvRef{}, false, nil
	}
	ref, err = orgAnthropicKVRef(obj)
	return ref, err == nil, err
}
