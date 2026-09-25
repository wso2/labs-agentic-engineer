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
// (dependency_json.go) plus ONE registry lookup: what is on disk decides, so a
// stale flag can never contradict the files. Precedence, first match wins:
//
//  1. Ref set, registry has a REGISTERED resource of that name
//                                   → continue at 4 (the copy carries the
//                                     provider; only the contract is checked)
//  2. Ref set, no such resource     → unresolved / needs-input ("The organization
//                                     has no registered resource with this name")
//  3. no Provider                   → unresolved / needs-input (no service chosen;
//     Suggestions may be open — the user chooses, the agent never does)
//  4. no contract file on disk      → unresolved / needs-contract
//  5. contract assumed, not yet
//     accepted by a user            → unresolved / needs-acceptance
//  6. otherwise                     → resolved; flag registered when the copy
//     came from the registry, assumed when the contract is agent-written under
//     the user's permission, derived when it was written from the provider's
//     own documentation, stale when the registry's document hash no longer
//     matches the copy's provenance.
//
// Style is not a rule: it is computed from the contract type, so a dependency
// with a contract always has one. The build gate blocks on unresolved and on
// nothing else: an assumed, derived or stale dependency builds, flagged
// everywhere it appears.

package spec

// OrgServiceHit is the org-service resolver's answer for one dependency name:
// Visible (reachable from this project's namespace) and Exists (in the org
// catalog at all).
type OrgServiceHit struct {
	Visible bool
	Exists  bool
}

// RegistryHit is the org resource registry's answer for one dependency name.
// Registered is true only for a resource an organization registered (a type
// with scope org) — never for the type a project's build left behind.
// DocumentSHA256 is the hash of the registered resource's contract document,
// when it has one, so a project copy can be compared against it.
type RegistryHit struct {
	Registered     bool
	DocumentSHA256 string
}

// ComputeDependencyStatus returns the read-time (status, reason) pair for one
// dependency. ComputeDependencyFlags is its companion for the qualifiers on a
// resolved external dependency; ApplyDependencyStatus sets all three.
func ComputeDependencyStatus(dep Dependency, registry RegistryHit, orgSvc OrgServiceHit) (status, reason string) {
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
		case dep.ResourceRef != "" && !registry.Registered:
			return DependencyStatusUnresolved, DependencyReasonNeedsInput
		case dep.ResourceRef == "" && dep.Provider == "":
			return DependencyStatusUnresolved, DependencyReasonNeedsInput
		case dep.Contract == "" && dep.SDK == "":
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
func ComputeDependencyFlags(dep Dependency, registry RegistryHit) []string {
	if dep.Kind != DependencyKindExternal {
		return nil
	}
	if status, _ := ComputeDependencyStatus(dep, registry, OrgServiceHit{}); status != DependencyStatusResolved {
		return nil
	}
	var flags []string
	if dep.ResourceRef != "" {
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
	if dep.ResourceRef != "" && registry.DocumentSHA256 != "" && dep.Provenance != nil &&
		dep.Provenance.SHA256 != "" && dep.Provenance.SHA256 != registry.DocumentSHA256 {
		flags = append(flags, DependencyFlagStale)
	}
	return flags
}

// ApplyDependencyStatus stamps Status, Reason and Flags on dep in place.
func ApplyDependencyStatus(dep *Dependency, registry RegistryHit, orgSvc OrgServiceHit) {
	dep.Status, dep.Reason = ComputeDependencyStatus(*dep, registry, orgSvc)
	dep.Flags = ComputeDependencyFlags(*dep, registry)
}
