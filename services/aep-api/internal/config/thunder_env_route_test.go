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

package config

import "testing"

// The two addresses of an environment's Thunder are not interchangeable, and
// neither resolves from where the other is right: a pod cannot resolve the
// public `*.amp.localhost` issuer, and a process outside the cluster cannot
// resolve the binding's `*.svc.cluster.local` Service. So the default has to
// follow where this process runs, not a single hardcoded choice — the wrong one
// reaches nothing at all.

func TestThunderEnvAdminRoute_InClusterDefaultsToTheBindingAddress(t *testing.T) {
	t.Setenv("KUBERNETES_SERVICE_HOST", "10.0.0.1")
	t.Setenv("THUNDER_ENV_ADMIN_ROUTE", "")

	if got := (&configReader{}).thunderEnvAdminRoute(); got != "binding" {
		t.Errorf("in a pod the route defaults to %q, want \"binding\" — the public issuer does not resolve there", got)
	}
}

func TestThunderEnvAdminRoute_OutsideTheClusterDefaultsToTheIssuer(t *testing.T) {
	t.Setenv("KUBERNETES_SERVICE_HOST", "")
	t.Setenv("THUNDER_ENV_ADMIN_ROUTE", "")

	if got := (&configReader{}).thunderEnvAdminRoute(); got != "issuer" {
		t.Errorf("outside the cluster the route defaults to %q, want \"issuer\" — the in-cluster Service does not resolve there", got)
	}
}

// An explicit setting wins on both sides of that line, for a deployment that
// sits on neither.
func TestThunderEnvAdminRoute_ExplicitSettingWins(t *testing.T) {
	for _, tc := range []struct{ kubeHost, set string }{
		{kubeHost: "10.0.0.1", set: "issuer"},
		{kubeHost: "", set: "binding"},
	} {
		t.Setenv("KUBERNETES_SERVICE_HOST", tc.kubeHost)
		t.Setenv("THUNDER_ENV_ADMIN_ROUTE", tc.set)

		if got := (&configReader{}).thunderEnvAdminRoute(); got != tc.set {
			t.Errorf("with KUBERNETES_SERVICE_HOST=%q the explicit %q was overridden to %q",
				tc.kubeHost, tc.set, got)
		}
	}
}
