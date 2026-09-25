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

// Package componentbuild serves the component build + deploy read surface for
// the bound org — trigger/list builds, build logs, the deploy list, and the
// component OpenAPI spec behind the console's Builds and Deployments pages.
//
// Triggers: trigger-build, list-builds, get-build-logs, list-deployments, get-component-openapi.
// Ports:    projects.ComponentService (the component read + build service).
package componentbuild
