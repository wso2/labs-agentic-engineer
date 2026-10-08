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
	"errors"
	"reflect"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
)

// stubRoleClient serves one rca-agent role (nil = not found) and records the
// patches sent to it.
type stubRoleClient struct {
	role     *unstructured.Unstructured
	getErr   error
	patchErr error
	patches  []string
}

func (s *stubRoleClient) Get(_ context.Context, apiVersion, kind, _, name string) (*unstructured.Unstructured, error) {
	if apiVersion != rcaAgentRoleAPIVersion || kind != rcaAgentRoleKind || name != rcaAgentRoleName {
		return nil, errors.New("unexpected object " + kind + "/" + name)
	}
	return s.role, s.getErr
}

func (s *stubRoleClient) Patch(_ context.Context, _, _, _, _ string, patchType types.PatchType, body []byte) error {
	if patchType != types.JSONPatchType {
		return errors.New("want a JSON patch, got " + string(patchType))
	}
	s.patches = append(s.patches, string(body))
	return s.patchErr
}

func roleWithActions(actions ...string) *unstructured.Unstructured {
	spec := map[string]any{}
	if actions != nil {
		list := make([]any, len(actions))
		for i, a := range actions {
			list[i] = a
		}
		spec["actions"] = list
	}
	return &unstructured.Unstructured{Object: map[string]any{"spec": spec}}
}

func TestMissingActions(t *testing.T) {
	want := []string{"resource:view", "resourcereleasebinding:view"}
	for _, tc := range []struct {
		name string
		have []string
		want []string
	}{
		{"none held", []string{"component:view"}, want},
		{"one held", []string{"resource:view"}, []string{"resourcereleasebinding:view"}},
		{"all held", []string{"resourcereleasebinding:view", "resource:view"}, nil},
		{"duplicates in have", []string{"resource:view", "resource:view"}, []string{"resourcereleasebinding:view"}},
		{"empty have", nil, want},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := missingActions(tc.have, want); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("missingActions(%v) = %v, want %v", tc.have, got, tc.want)
			}
		})
	}
}

func TestEnsureRCAAgentRole(t *testing.T) {
	ctx := context.Background()

	t.Run("already complete sends no patch", func(t *testing.T) {
		rc := &stubRoleClient{role: roleWithActions("component:view", "resource:view", "resourcereleasebinding:view")}
		added, err := ensureRCAAgentRole(ctx, rc)
		if err != nil || added != nil || len(rc.patches) != 0 {
			t.Fatalf("added=%v err=%v patches=%v, want nothing", added, err, rc.patches)
		}
	})

	t.Run("appends both missing actions in one patch", func(t *testing.T) {
		rc := &stubRoleClient{role: roleWithActions("component:view")}
		added, err := ensureRCAAgentRole(ctx, rc)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(added, rcaAgentActions) {
			t.Errorf("added = %v", added)
		}
		want := `[{"op":"add","path":"/spec/actions/-","value":"resource:view"},{"op":"add","path":"/spec/actions/-","value":"resourcereleasebinding:view"}]`
		if len(rc.patches) != 1 || rc.patches[0] != want {
			t.Errorf("patches = %v, want [%s]", rc.patches, want)
		}
	})

	t.Run("appends only the missing one", func(t *testing.T) {
		rc := &stubRoleClient{role: roleWithActions("resourcereleasebinding:view")}
		added, err := ensureRCAAgentRole(ctx, rc)
		if err != nil {
			t.Fatal(err)
		}
		want := `[{"op":"add","path":"/spec/actions/-","value":"resource:view"}]`
		if !reflect.DeepEqual(added, []string{"resource:view"}) || len(rc.patches) != 1 || rc.patches[0] != want {
			t.Errorf("added=%v patches=%v", added, rc.patches)
		}
	})

	t.Run("a missing role is an error, with no patch", func(t *testing.T) {
		rc := &stubRoleClient{}
		if _, err := ensureRCAAgentRole(ctx, rc); !errors.Is(err, errRCAAgentRoleNotFound) || len(rc.patches) != 0 {
			t.Errorf("err=%v patches=%v", err, rc.patches)
		}
	})

	t.Run("get and patch failures are returned", func(t *testing.T) {
		forbidden := errors.New("forbidden")
		if _, err := ensureRCAAgentRole(ctx, &stubRoleClient{getErr: forbidden}); !errors.Is(err, forbidden) {
			t.Errorf("get: err = %v", err)
		}
		rc := &stubRoleClient{role: roleWithActions("component:view"), patchErr: forbidden}
		if added, err := ensureRCAAgentRole(ctx, rc); !errors.Is(err, forbidden) || added != nil {
			t.Errorf("patch: added=%v err=%v", added, err)
		}
	})
}

func TestRCAAgentRoleManualPatch(t *testing.T) {
	got := rcaAgentRoleManualPatch()
	for _, want := range []string{
		"kubectl patch clusterauthzrole.openchoreo.dev rca-agent --type=json -p '",
		`"value":"resource:view"`,
		`"value":"resourcereleasebinding:view"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("manual patch %q lacks %q", got, want)
		}
	}
}
