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
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const repoCatalog = "../../../../../deployments/single-cluster/resource-types"

func outputNames(rt ResourceType) []string {
	var names []string
	for _, o := range rt.Spec.Outputs {
		names = append(names, o.Name)
	}
	return names
}

func TestResourceTypeDir_DecodesTheRepoCatalog(t *testing.T) {
	t.Parallel()
	types, err := ResourceTypeDir(repoCatalog).ListClusterResourceTypes(context.Background())
	if err != nil {
		t.Fatalf("ListClusterResourceTypes: %v", err)
	}
	byName := map[string]ResourceType{}
	for _, rt := range types {
		byName[rt.Metadata.Name] = rt
	}
	if len(byName) != 2 {
		t.Fatalf("want postgres-cnpg and thunder-app, got %v", reflect.ValueOf(byName).MapKeys())
	}

	thunder := byName["thunder-app"]
	if thunder.Kind != "ClusterResourceType" || thunder.APIVersion != "openchoreo.dev/v1alpha1" {
		t.Errorf("thunder-app: kind/apiVersion = %s %s", thunder.Kind, thunder.APIVersion)
	}
	if got, want := outputNames(thunder), []string{"client_id", "issuer", "jwks_url", "scopes", "resource"}; !reflect.DeepEqual(got, want) {
		t.Errorf("thunder-app outputs = %v, want %v", got, want)
	}
	if thunder.Metadata.Labels["aep.wso2.com/role"] != "end-user-auth" {
		t.Errorf("thunder-app role label = %q", thunder.Metadata.Labels["aep.wso2.com/role"])
	}
	if thunder.Metadata.Annotations["aep.wso2.com/skill"] != "thunder-authentication" {
		t.Errorf("thunder-app skill annotation = %q", thunder.Metadata.Annotations["aep.wso2.com/skill"])
	}
	if len(thunder.Spec.Resources) == 0 || len(thunder.Spec.Resources[0].Template) == 0 {
		t.Errorf("thunder-app: resource template not decoded")
	}

	pg := byName["postgres-cnpg"]
	if got, want := outputNames(pg), []string{"host", "port", "dbname", "user", "password"}; !reflect.DeepEqual(got, want) {
		t.Errorf("postgres-cnpg outputs = %v, want %v", got, want)
	}
	if pg.Spec.Outputs[4].SecretKeyRef == nil || pg.Spec.Outputs[4].SecretKeyRef.Key != "password" {
		t.Errorf("postgres-cnpg password output: secretKeyRef not decoded: %+v", pg.Spec.Outputs[4])
	}
	if pg.Spec.Parameters == nil || pg.Spec.Parameters.OpenAPIV3Schema["properties"] == nil {
		t.Errorf("postgres-cnpg: parameter schema not decoded")
	}
	if pg.Metadata.Labels["aep.wso2.com/role"] != "" {
		t.Errorf("postgres-cnpg must carry no role label")
	}
}

func TestResourceTypeDir_SkipsOtherKinds(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	manifest := strings.Join([]string{
		"# header comment only",
		"---",
		"apiVersion: rbac.authorization.k8s.io/v1",
		"kind: ClusterRole",
		"metadata: {name: not-a-type}",
		"---",
		"apiVersion: openchoreo.dev/v1alpha1",
		"kind: ResourceType",
		"metadata: {name: cache}",
		"spec:",
		"  outputs: [{name: url, value: x}]",
		"  resources: []",
		"",
	}, "\n")
	if err := os.MkdirAll(filepath.Join(dir, "cache"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cache", ResourceTypeFileName), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	types, err := ResourceTypeDir(dir).ListClusterResourceTypes(context.Background())
	if err != nil {
		t.Fatalf("ListClusterResourceTypes: %v", err)
	}
	if len(types) != 1 || types[0].Metadata.Name != "cache" || outputNames(types[0])[0] != "url" {
		t.Fatalf("want only the cache type, got %+v", types)
	}
}

func TestResourceTypeDir_EmptyDirectoryIsAnError(t *testing.T) {
	t.Parallel()
	if _, err := ResourceTypeDir(t.TempDir()).ListClusterResourceTypes(context.Background()); err == nil {
		t.Fatal("want an error for a directory with no resourcetype.yaml")
	}
}
