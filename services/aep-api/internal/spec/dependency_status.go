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

// dependency_status.go — the single resolution authority for a dependency's
// read-time Status/Reason/Flags (ADR-0003: never authored, never persisted).
//
// For an external dependency the state is read off its hydrated definition
// (dependency_json.go): what is on disk decides, so a stale flag can never
// contradict the files. Precedence, first match wins:
//
//  1. Source "org" or registry hit  → resolved, flag registered
//  2. no Provider                   → unresolved / needs-input (no service chosen;
//     Suggestions may be open — the user chooses, the agent never does)
//  3. no Style                      → unresolved / needs-input (a provider named, its shape not)
//  4. sdk with no manifest on disk  → unresolved / needs-contract
//  5. rest-api/graphql, no contract → unresolved / needs-contract
//  6. contract agent-written, not
//     yet accepted by a user          → unresolved / needs-acceptance
//  7. otherwise                     → resolved; flag assumed when the contract
//     is agent-written under the user's permission, flag derived when it was
//     written from the provider's own documentation (no permission needed),
//     flag sdk-only when an sdk dependency has no API contract beside its
//     manifest.
//
// The build gate blocks on unresolved and on nothing else: an
// assumed or sdk-only dependency builds, flagged everywhere it appears.

package spec

// OrgServiceHit is the org-service resolver's answer for one dependency name:
// Visible (reachable from this project's namespace) and Exists (in the org
// catalog at all).
type OrgServiceHit struct {
	Visible bool
	Exists  bool
}

// ComputeDependencyStatus returns the read-time (status, reason) pair for one
// dependency. ComputeDependencyFlags is its companion for the qualifiers on a
// resolved external dependency; ApplyDependencyStatus sets all three.
func ComputeDependencyStatus(dep Dependency, registryHit bool, orgSvc OrgServiceHit) (status, reason string) {
	switch dep.Kind {
	case DependencyKindComponent, DependencyKindPlatformResource:
		return DependencyStatusResolved, ""

	case DependencyKindOrgService:
		if orgSvc.Visible {
			return DependencyStatusResolved, ""
		}
		if orgSvc.Exists {
			return DependencyStatusBlocked, DependencyReasonAccessRequired
		}
		return DependencyStatusUnresolved, DependencyReasonNotFound

	case DependencyKindExternal:
		switch {
		case dep.Source == DependencySourceOrg || registryHit:
			return DependencyStatusResolved, ""
		case dep.Provider == "":
			return DependencyStatusUnresolved, DependencyReasonNeedsInput
		case dep.Style == "":
			return DependencyStatusUnresolved, DependencyReasonNeedsInput
		case dep.Style == DependencyStyleSDK && dep.SDK == "":
			return DependencyStatusUnresolved, DependencyReasonNeedsContract
		case dep.Style != DependencyStyleSDK && dep.Contract == "":
			return DependencyStatusUnresolved, DependencyReasonNeedsContract
		case dep.ContractAssumed && dep.Assumed == nil:
			return DependencyStatusUnresolved, DependencyReasonNeedsAcceptance
		default:
			return DependencyStatusResolved, ""
		}

	default:
		return DependencyStatusResolved, ""
	}
}

// ComputeDependencyFlags returns the qualifiers on a RESOLVED external
// dependency, in a fixed order; nil for anything else.
func ComputeDependencyFlags(dep Dependency, registryHit bool) []string {
	if dep.Kind != DependencyKindExternal {
		return nil
	}
	if status, _ := ComputeDependencyStatus(dep, registryHit, OrgServiceHit{}); status != DependencyStatusResolved {
		return nil
	}
	var flags []string
	if dep.Source == DependencySourceOrg || registryHit {
		flags = append(flags, DependencyFlagRegistered)
	}
	// Assumed means the contract on disk is the agent-written one AND the user
	// accepted it; a record echoed beside a real document is not a flag.
	if dep.ContractAssumed && dep.Assumed != nil {
		flags = append(flags, DependencyFlagAssumed)
	}
	if dep.ContractDerived {
		flags = append(flags, DependencyFlagDerived)
	}
	if dep.Style == DependencyStyleSDK && dep.Contract == "" {
		flags = append(flags, DependencyFlagSDKOnly)
	}
	return flags
}

// ApplyDependencyStatus stamps Status, Reason and Flags on dep in place.
func ApplyDependencyStatus(dep *Dependency, registryHit bool, orgSvc OrgServiceHit) {
	dep.Status, dep.Reason = ComputeDependencyStatus(*dep, registryHit, orgSvc)
	dep.Flags = ComputeDependencyFlags(*dep, registryHit)
}
