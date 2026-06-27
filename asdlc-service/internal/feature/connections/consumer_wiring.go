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

package connections

import (
	"strings"
)

// Consumer-side connection wiring is now declarative: the coding agent writes
// `spec.dependencies` into its own `workload.yaml` at dispatch time (the
// dispatch-time resolver in codingagent/dispatch_service.go renders the deps
// from an issue comment). The post-deploy "patch the deployed Workload CR"
// path has been removed. The helpers below remain because the dispatch-time
// resolver derives the same names/env vars through these exported wrappers —
// single source of truth, shared by the declarative path.

// OrgServiceURLEnv is the exported wrapper over orgServiceURLEnv so callers
// outside this package (e.g. the dispatch-time consumer-dependency YAML
// renderer) derive the same `<NAME>_URL` env var OC binds the resolved
// org-service address to. Single source of truth — delegates, never forks.
func OrgServiceURLEnv(name string) string { return orgServiceURLEnv(name) }

// ConnectionBindingName is the exported wrapper over connectionBindingName —
// the per-env ResourceReleaseBinding name a connection's outputs are read from.
func ConnectionBindingName(project, conn, env string) string {
	return connectionBindingName(project, conn, env)
}

// ConnectionResourceName is the exported wrapper over connectionResourceName —
// the OC Resource name (== Workload dependency `ref`) for a project connection.
func ConnectionResourceName(project, conn string) string {
	return connectionResourceName(project, conn)
}

// orgServiceURLEnv is the consumer env var OC binds the resolved org-service
// address to — same `<UPPER_SNAKE>_URL` convention SPAs read via window._env_,
// so a Go/Node consumer reads e.g. EMPLOYEE_API_URL regardless of how the URL
// is delivered.
func orgServiceURLEnv(name string) string {
	var b strings.Builder
	prevAlnum := false
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r - 'a' + 'A')
			prevAlnum = true
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevAlnum = true
		case r == '-' || r == '_':
			if prevAlnum {
				b.WriteByte('_')
			}
			prevAlnum = false
		}
	}
	return strings.TrimSuffix(b.String(), "_") + "_URL"
}

// connectionBindingName mirrors the provisioner's per-env binding name.
func connectionBindingName(project, conn, env string) string {
	return connectionResourceName(project, conn) + "-" + env
}
