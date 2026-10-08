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

// The v1.3.0 SRE agent's Resource and ResourceReleaseBinding tools need two
// actions OpenChoreo 1.2.5's rca-agent role lacks. The role is a list item in
// the control-plane chart's values, which Helm cannot merge into, so aectl
// patches it (deployments/helm-charts/design/sre-agent-install.md).

package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"

	"github.com/wso2/aep/aectl/internal/ui"
)

const (
	rcaAgentRoleAPIVersion = "openchoreo.dev/v1alpha1"
	rcaAgentRoleKind       = "ClusterAuthzRole"
	rcaAgentRoleName       = "rca-agent"
)

var rcaAgentActions = []string{"resource:view", "resourcereleasebinding:view"}

var errRCAAgentRoleNotFound = errors.New("ClusterAuthzRole rca-agent not found")

// roleClient is the part of *kubernetes.Applier the role step needs.
type roleClient interface {
	Get(ctx context.Context, apiVersion, kind, namespace, name string) (*unstructured.Unstructured, error)
	Patch(ctx context.Context, apiVersion, kind, namespace, name string, patchType types.PatchType, body []byte) error
}

// ensureRCAAgentRole adds the rcaAgentActions the role lacks, in one patch,
// and returns them.
func ensureRCAAgentRole(ctx context.Context, rc roleClient) ([]string, error) {
	role, err := rc.Get(ctx, rcaAgentRoleAPIVersion, rcaAgentRoleKind, "", rcaAgentRoleName)
	if err != nil {
		return nil, err
	}
	if role == nil {
		return nil, errRCAAgentRoleNotFound
	}
	have, _, err := unstructured.NestedStringSlice(role.Object, "spec", "actions")
	if err != nil {
		return nil, fmt.Errorf("read %s spec.actions: %w", rcaAgentRoleName, err)
	}
	missing := missingActions(have, rcaAgentActions)
	if len(missing) == 0 {
		return nil, nil
	}
	if err := rc.Patch(ctx, rcaAgentRoleAPIVersion, rcaAgentRoleKind, "", rcaAgentRoleName, types.JSONPatchType, rcaAgentRolePatch(missing)); err != nil {
		return nil, err
	}
	return missing, nil
}

func missingActions(have, want []string) []string {
	held := make(map[string]bool, len(have))
	for _, a := range have {
		held[a] = true
	}
	var missing []string
	for _, a := range want {
		if !held[a] {
			missing = append(missing, a)
		}
	}
	return missing
}

// rcaAgentRolePatch appends each action to the role's spec.actions.
func rcaAgentRolePatch(actions []string) []byte {
	type op struct {
		Op    string `json:"op"`
		Path  string `json:"path"`
		Value string `json:"value"`
	}
	ops := make([]op, 0, len(actions))
	for _, a := range actions {
		ops = append(ops, op{Op: "add", Path: "/spec/actions/-", Value: a})
	}
	body, _ := json.Marshal(ops) // cannot fail: a slice of string-only structs
	return body
}

// reportRCAAgentRole prints the role step's outcome. A failure only warns: the
// agent still completes RCA and remediation without the two actions.
func reportRCAAgentRole(added []string, err error) {
	switch {
	case err != nil:
		ui.Warn(strings.Join([]string{
			fmt.Sprintf("Could not add %s to the %s role: %v.", strings.Join(rcaAgentActions, " and "), rcaAgentRoleName, err),
			"The SRE agent can't use get_resource, list_resource_release_bindings or get_resource_release_binding, so RCA can't see Resources or ResourceReleaseBindings.",
			"To grant them, add any action the role lacks: " + rcaAgentRoleManualPatch(),
		}, " "))
	case len(added) > 0:
		ui.Detail(fmt.Sprintf("%s += %s", rcaAgentRoleName, strings.Join(added, ", ")))
	default:
		ui.Detail(fmt.Sprintf("%s already covers the SRE agent's tools", rcaAgentRoleName))
	}
}

// rcaAgentRoleManualPatch is the kubectl command an operator runs when aectl
// cannot patch the role itself.
func rcaAgentRoleManualPatch() string {
	return fmt.Sprintf("kubectl patch clusterauthzrole.openchoreo.dev %s --type=json -p '%s'", rcaAgentRoleName, rcaAgentRolePatch(rcaAgentActions))
}
