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

// retirement_test.go holds the invariants that lock the retired dispatch model
// out of the tree. Coding-agent cycles are OpenChoreo Components created
// through the OC API; aep-api reaches no cluster any other way. These tests are
// the reason a future session cannot quietly reintroduce a Kubernetes client or
// a cluster-gateway-proxy call — the boundary is executable, not documented.
package arch

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestCodingAgentDispatchesOnlyThroughOpenChoreo asserts the delivery/codingagent
// package holds no DIRECT edge to the cluster-gateway-proxy client or to a
// controller-runtime Kubernetes client. Direct (not transitive) is the right
// granularity here: sibling domains may still legitimately import packages that
// this one must not touch itself.
func TestCodingAgentDispatchesOnlyThroughOpenChoreo(t *testing.T) {
	const pkg = mod + "/internal/delivery/codingagent"
	banned := map[string]string{
		mod + "/internal/clients/clustergatewayproxy": "cycle dispatch and log reads go through the OpenChoreo API; the proxy path is retired",
		"sigs.k8s.io/controller-runtime/pkg/client":   "OpenChoreo renders the cycle Job; aep-api never talks to a Kubernetes API itself",
	}
	for _, imp := range directImports(t, pkg) {
		if why, bad := banned[imp]; bad {
			t.Errorf("delivery/codingagent imports %s — %s", imp, why)
		}
	}
}

// TestClusterGatewayProxyClientIsDeleted asserts the proxy client package does
// not exist on disk — the strongest form of the boundary, since a re-created
// package fails here before anything imports it. app-factory is no longer a
// cluster-gateway-proxy caller: the proxy itself survives in wso2cloud for other
// platforms, but nothing in this repo speaks to it.
func TestClusterGatewayProxyClientIsDeleted(t *testing.T) {
	const pkg = mod + "/internal/clients/clustergatewayproxy"
	if err := exec.Command("go", "list", pkg).Run(); err == nil {
		t.Errorf("%s still resolves — the cluster-gateway-proxy client must stay deleted", pkg)
	}
}

// TestNoInClusterKubernetesClient asserts the aep-api binary pulls in no
// Kubernetes client at all. Every cluster-side effect — the cycle Job, its
// secrets, the build credential — is authored through the OpenChoreo API, which
// is what keeps a single audited write path and lets the deployment run without
// cluster RBAC of its own.
func TestNoInClusterKubernetesClient(t *testing.T) {
	const main = mod + "/cmd/aep-api"
	banned := map[string]string{
		"sigs.k8s.io/controller-runtime/pkg/client": "OpenChoreo owns every cluster write; aep-api holds no Kubernetes client",
		mod + "/internal/clients/k8s":               "the in-cluster client wrapper is retired with the last SSA writer",
	}
	for dep, why := range banned {
		if imports(t, main, dep) {
			t.Errorf("cmd/aep-api reaches %s — %s", dep, why)
		}
	}
}

// TestRemoteWorkerNamespaceIsGone asserts no Go source in the module names the
// retired per-org `-remote-worker` namespace. Cycle Jobs render into the
// project's own `dp-…` dataplane namespace, so nothing derives that name any
// more. The needle is assembled at compile time so this file does not match
// itself.
func TestRemoteWorkerNamespaceIsGone(t *testing.T) {
	needle := "RemoteWorker" + "Namespace"
	var hits []string
	root := "../.."
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "testdata":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		if filepath.Base(path) == "retirement_test.go" {
			return nil
		}
		b, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if strings.Contains(string(b), needle) {
			hits = append(hits, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	if len(hits) > 0 {
		t.Errorf("%s is still referenced in %v — cycle Jobs render into the project's dp-… dataplane namespace; the -remote-worker namespace is retired", needle, hits)
	}
}
