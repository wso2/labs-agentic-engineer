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

// markers.go is the ONE shared definition of the `aep.wso2.com/*` CRT
// metadata vocabulary — the PE-authored labels and annotations aep-api keys
// behavior on instead of hardcoded resourceType names (see
// learning/thunder-resource/PLAN-generalization.md, "The CRT metadata
// vocabulary"). Every consumer (design-save's auth derivation, runtimeconfig's
// consumer-URL patch, skill auto-attach) extracts markers through
// MarkersFrom / the catalog's MarkersByName rather than branching on a
// ClusterResourceType's name.

const (
	// LabelRole is the label key a PE-authored ClusterResourceType carries to
	// declare a semantic role. The only recognized value today is
	// RoleEndUserAuth.
	LabelRole = "aep.wso2.com/role"

	// RoleEndUserAuth is the LabelRole value marking a resource type whose
	// dependencies require end-user authentication. Design save stamps
	// `exposesAPI.auth: end-user-required` on any component declaring a dep
	// of a type carrying this role (conflicting with an explicit
	// `service-required` is a validation error).
	RoleEndUserAuth = "end-user-auth"

	// AnnotationConsumerURLEnvConfig names the env-config key that should
	// receive the consuming web-app's public callback URL once it resolves.
	// When present, the platform patches `<origin><consumer-url-path>` into
	// this key on the dependency's dev ResourceReleaseBinding via
	// PatchBindingEnvironmentConfigs.
	AnnotationConsumerURLEnvConfig = "aep.wso2.com/consumer-url-env-config"

	// AnnotationConsumerURLPath is the path appended to the consumer's
	// origin for the patch driven by AnnotationConsumerURLEnvConfig. Only
	// meaningful alongside that annotation; when absent there,
	// DefaultConsumerURLPath applies.
	AnnotationConsumerURLPath = "aep.wso2.com/consumer-url-path"

	// DefaultConsumerURLPath is the path used when
	// AnnotationConsumerURLEnvConfig is present but AnnotationConsumerURLPath
	// is not.
	DefaultConsumerURLPath = "/callback"

	// AnnotationSkill names a skill that must be present in a design's
	// skillsApplied whenever a dependency of this resource type exists.
	// Design save appends it (append-only; unknown skill names warn, they
	// never fail the save).
	AnnotationSkill = "aep.wso2.com/skill"

	// AnnotationDescription carries human prose describing what the resource
	// type provides and when a component should depend on it. OpenChoreo's
	// ClusterResourceType spec has no native description field (and its CRD
	// rejects unknown fields), so the annotation is the mechanism. Surfaced
	// to the architect via the list_platform_resource_types MCP tool and to
	// users in the console dependency drawer.
	AnnotationDescription = "aep.wso2.com/description"
)

// TypeMarkers is the extracted, typed view of the `aep.wso2.com/*` vocabulary
// carried by one ClusterResourceType's labels/annotations. The zero value
// means "no markers" — every consumer can test it directly (e.g.
// `markers.EndUserAuth`, `markers.ConsumerURLEnvConfig != ""`,
// `markers.Skill != ""`) without a hardcoded resourceType name anywhere.
type TypeMarkers struct {
	// EndUserAuth is true when the type carries LabelRole=RoleEndUserAuth.
	EndUserAuth bool
	// ConsumerURLEnvConfig is the env-config key to patch the consumer's
	// callback URL into, or "" when the type carries no such annotation.
	ConsumerURLEnvConfig string
	// ConsumerURLPath is the path appended to the consumer's origin for the
	// patch above. Defaults to DefaultConsumerURLPath when
	// ConsumerURLEnvConfig is set but no explicit path annotation is present.
	ConsumerURLPath string
	// Skill is the skill name that must appear in skillsApplied, or "" when
	// the type carries no skill annotation.
	Skill string
	// Description is the human prose off AnnotationDescription — what the
	// type provides and when to depend on it — or "" when the type carries
	// no description annotation.
	Description string
}

// MarkersFrom extracts a TypeMarkers from a ClusterResourceType's raw
// metadata.labels / metadata.annotations maps. Both may be nil (a Go nil map
// read is a safe zero-value lookup) — a type with no markers at all yields
// the zero-value TypeMarkers.
func MarkersFrom(labels, annotations map[string]string) TypeMarkers {
	m := TypeMarkers{
		EndUserAuth:          labels[LabelRole] == RoleEndUserAuth,
		ConsumerURLEnvConfig: annotations[AnnotationConsumerURLEnvConfig],
		ConsumerURLPath:      annotations[AnnotationConsumerURLPath],
		Skill:                annotations[AnnotationSkill],
		Description:          annotations[AnnotationDescription],
	}
	if m.ConsumerURLEnvConfig != "" && m.ConsumerURLPath == "" {
		m.ConsumerURLPath = DefaultConsumerURLPath
	}
	return m
}
