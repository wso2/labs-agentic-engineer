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

package dependencies

import "errors"

// ErrNotRegistered is returned when a value submission names an external
// resource the org has not registered. Endpoint layers map it to 404.
var ErrNotRegistered = errors.New("external resource is not registered for the org")

// Sentinels for the platform-resource provision/status flow. The HTTP layer
// maps them: ErrDepNotFound → 404, ErrDepWrongKind → 400 (its wrap message
// names the dep's actual kind and the applicable one), ErrProvisionFailed →
// 502.
var (
	// ErrDepNotFound is returned when the named dependency (or its component,
	// or the whole design) is absent from the project's design.
	ErrDepNotFound = errors.New("platform-resource dependency not found")

	// ErrDepWrongKind is returned when the named dependency exists but is not
	// a platform-resource — the requested action does not apply to its kind.
	ErrDepWrongKind = errors.New("dependency kind does not support this action")

	// ErrProvisionFailed is returned when the ResourceProvisioner call fails.
	ErrProvisionFailed = errors.New("platform provisioner failed")

	// ErrProvisionPermanent marks a provisioning answer that retrying cannot
	// change: the ClusterResourceType does not exist, or the Resource never
	// cuts a release. Distinct from ErrProvisionFailed, which is also used
	// for blips (OC down) that must stay retryable.
	ErrProvisionPermanent = errors.New("platform provisioner failed permanently")
)
