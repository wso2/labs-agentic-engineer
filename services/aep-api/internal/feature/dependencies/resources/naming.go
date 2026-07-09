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

package resources

import "strings"

// EnvVarName builds a valid C_IDENTIFIER env-var name from a dependency name +
// output name (join with "_", map every char outside [A-Za-z0-9_] to '_',
// upper-case). It is the SINGLE source of truth for the platform-resource
// output naming convention: the provisioning wiring (pod env-var injection in
// wiring.go) and the SPA runtime config (window._env_ keys in runtimeconfig)
// both derive their keys through it, so the coding agent and the browser see
// byte-identical names. e.g. "orders-db" + "host" → "ORDERS_DB_HOST";
// "user-auth" + "client_id" → "USER_AUTH_CLIENT_ID".
func EnvVarName(depName, outName string) string {
	joined := depName + "_" + outName
	mapped := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_':
			return r
		default:
			return '_'
		}
	}, joined)
	return strings.ToUpper(mapped)
}

// ExternalResourceName is the per-project OC Resource name (== the Workload
// dependency `ref`) for a project's external resource. metadata.name is
// namespace-unique — owner.projectName does NOT scope it — so the project
// prefixes the name. Exported: the dispatch-time consumer-dependency renderer
// derives the same name through this single source of truth.
func ExternalResourceName(project, name string) string { return project + "-" + name }

// ExternalResourceBindingName is the per-env ResourceReleaseBinding name an
// external resource's outputs are read from — mirrors the provisioner's
// binding naming.
func ExternalResourceBindingName(project, name, env string) string {
	return ExternalResourceName(project, name) + "-" + env
}
