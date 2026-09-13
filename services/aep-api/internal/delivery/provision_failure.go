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

package delivery

import (
	"fmt"
	"strings"
)

// ProvisionFault is one dependency the version's gate provisioning could not
// author, as the composition root hands it up from the provisioning feature:
// which component declared it, which dependency, the provisioner's own words,
// and whether repeating could change the answer.
type ProvisionFault struct {
	Component  string
	Dependency string
	Reason     string
	Permanent  bool
	// Cause is the provisioner's own error, kept so errors.Is on the way out
	// still sees the dependencies domain's sentinels through Unwrap.
	Cause error
}

// ProvisionFailedError is the error ProvisionGates' resolver returns when one or
// more dependencies failed. It keeps the per-dependency facts that used to be
// flattened into one string at the composition root, so the run can record
// WHICH dependency failed and why (RunFailure) while the wrapped text and the
// permanent/transient classification stay exactly what they were.
type ProvisionFailedError struct {
	Faults []ProvisionFault
}

// Error is the joined text the log and Temporal history carried before.
func (e *ProvisionFailedError) Error() string {
	reasons := make([]string, 0, len(e.Faults))
	for _, f := range e.Faults {
		reasons = append(reasons, f.Dependency+": "+f.Reason)
	}
	return fmt.Sprintf("provision %d dependenc(ies) failed: %s", len(e.Faults), strings.Join(reasons, "; "))
}

// Is makes errors.Is(err, ErrProvisionPermanent) true when ANY fault is
// permanent — the same collapse aggregateProvisionFailures made, kept so
// provisionErr's non-retryable classification is unchanged.
func (e *ProvisionFailedError) Is(target error) bool {
	if target != ErrProvisionPermanent {
		return false
	}
	for _, f := range e.Faults {
		if f.Permanent {
			return true
		}
	}
	return false
}

// Unwrap exposes every fault's cause, so errors.Is/As through this error reach
// the provisioner's own sentinels (the dependencies domain's
// ErrProvisionPermanent among them) exactly as the flattened wrap did.
func (e *ProvisionFailedError) Unwrap() []error {
	out := make([]error, 0, len(e.Faults))
	for _, f := range e.Faults {
		if f.Cause != nil {
			out = append(out, f.Cause)
		}
	}
	return out
}
