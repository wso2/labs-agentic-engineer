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

import "testing"

func TestMarkersFrom_AllMarkersPresent(t *testing.T) {
	t.Parallel()

	labels := map[string]string{LabelRole: RoleEndUserAuth}
	annotations := map[string]string{
		AnnotationConsumerURLEnvConfig: "redirectUris",
		AnnotationConsumerURLPath:      "/oauth2/callback",
		AnnotationSkill:                "thunder-authentication",
		AnnotationDescription:          "End-user sign-in: provisions an OAuth client on the platform IdP.",
	}

	got := MarkersFrom(labels, annotations)
	want := TypeMarkers{
		EndUserAuth:          true,
		ConsumerURLEnvConfig: "redirectUris",
		ConsumerURLPath:      "/oauth2/callback",
		Skill:                "thunder-authentication",
		Description:          "End-user sign-in: provisions an OAuth client on the platform IdP.",
	}
	if got != want {
		t.Fatalf("MarkersFrom() = %+v, want %+v", got, want)
	}
}

func TestMarkersFrom_AbsentMarkers_ZeroValue(t *testing.T) {
	t.Parallel()

	got := MarkersFrom(nil, nil)
	if got != (TypeMarkers{}) {
		t.Fatalf("MarkersFrom(nil, nil) = %+v, want zero value", got)
	}

	// Also zero-value when the maps are non-nil but carry unrelated keys.
	got = MarkersFrom(map[string]string{"other": "label"}, map[string]string{"other": "annotation"})
	if got != (TypeMarkers{}) {
		t.Fatalf("MarkersFrom(unrelated) = %+v, want zero value", got)
	}
}

func TestMarkersFrom_ConsumerURLEnvConfigWithoutPath_DefaultsCallback(t *testing.T) {
	t.Parallel()

	got := MarkersFrom(nil, map[string]string{AnnotationConsumerURLEnvConfig: "redirectUris"})
	if got.ConsumerURLEnvConfig != "redirectUris" {
		t.Fatalf("ConsumerURLEnvConfig = %q, want %q", got.ConsumerURLEnvConfig, "redirectUris")
	}
	if got.ConsumerURLPath != DefaultConsumerURLPath {
		t.Fatalf("ConsumerURLPath = %q, want default %q", got.ConsumerURLPath, DefaultConsumerURLPath)
	}
	if got.EndUserAuth {
		t.Fatalf("EndUserAuth = true, want false (no role label)")
	}
	if got.Skill != "" {
		t.Fatalf("Skill = %q, want empty", got.Skill)
	}
	if got.Description != "" {
		t.Fatalf("Description = %q, want empty", got.Description)
	}
}

func TestMarkersFrom_RoleLabelWithOtherValue_NotEndUserAuth(t *testing.T) {
	t.Parallel()

	got := MarkersFrom(map[string]string{LabelRole: "something-else"}, nil)
	if got.EndUserAuth {
		t.Fatalf("EndUserAuth = true for role %q, want false", "something-else")
	}
}
