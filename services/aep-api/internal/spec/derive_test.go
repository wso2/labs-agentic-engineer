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

package spec

import (
	"errors"
	"strings"
	"testing"
)

func assembled(t *testing.T, files map[string]string) *DesignFile {
	t.Helper()
	d, err := AssembleDesign(files)
	if err != nil {
		t.Fatalf("AssembleDesign: %v", err)
	}
	return d
}

func thunderAndPostgres() map[string]CRTType {
	return map[string]CRTType{
		"thunder-app":   {EndUserAuth: true, Outputs: []string{"client_id", "issuer"}},
		"postgres-cnpg": {Outputs: []string{"host", "port"}},
	}
}

func TestDerivePlatformResourceFacts_ReturnsTheChangedComponentRendered(t *testing.T) {
	t.Parallel()
	files := designFilesWithDeps(`[{"kind":"platform-resource","name":"user-auth","resourceType":"thunder-app"}]`)
	design := assembled(t, files)

	derived, err := DerivePlatformResourceFacts(design, thunderAndPostgres(), "web")
	if err != nil {
		t.Fatalf("DerivePlatformResourceFacts: %v", err)
	}
	if len(derived) != 1 || derived[0].Path != "components/consumer/design.json" || derived[0].CreateOnly {
		t.Fatalf("want one replace of the consumer design.json, got %+v", derived)
	}
	want, err := SplitDesign(&DesignFile{Components: design.Components})
	if err != nil {
		t.Fatalf("SplitDesign: %v", err)
	}
	if derived[0].Content != want["components/consumer/design.json"] {
		t.Fatalf("content is not production's render:\n%s", derived[0].Content)
	}
	for _, s := range []string{`"auth": "end-user-required"`, `"ref": "web-`, `"client_id": "USER_AUTH_CLIENT_ID"`} {
		if !strings.Contains(derived[0].Content, s) {
			t.Errorf("rendered design.json missing %s:\n%s", s, derived[0].Content)
		}
	}
}

func TestDerivePlatformResourceFacts_OwnOutputDerivesNothing(t *testing.T) {
	t.Parallel()
	files := designFilesWithDeps(`[{"kind":"platform-resource","name":"orders-db","resourceType":"postgres-cnpg"}]`)
	first, err := DerivePlatformResourceFacts(assembled(t, files), thunderAndPostgres(), "web")
	if err != nil || len(first) != 1 {
		t.Fatalf("first pass: files=%d err=%v", len(first), err)
	}
	files[first[0].Path] = first[0].Content
	second, err := DerivePlatformResourceFacts(assembled(t, files), thunderAndPostgres(), "web")
	if err != nil {
		t.Fatalf("second pass: %v", err)
	}
	if len(second) != 0 {
		t.Fatalf("a derived design must derive to nothing, got %+v", second)
	}
}

func TestDerivePlatformResourceFacts_UnknownTypeRefusesBeforeMutating(t *testing.T) {
	t.Parallel()
	design := assembled(t, designFilesWithDeps(`[
    {"kind":"platform-resource","name":"user-auth","resourceType":"thunder-app"},
    {"kind":"platform-resource","name":"receipts","resourceType":"object-storage"}]`))

	derived, err := DerivePlatformResourceFacts(design, thunderAndPostgres(), "web")
	if !errors.Is(err, ErrUnknownResourceType) || !strings.Contains(err.Error(), `receipts ("object-storage")`) {
		t.Fatalf("want ErrUnknownResourceType naming the dependency, got %v", err)
	}
	if derived != nil || design.Components[0].ExposesAPI != nil || design.Components[0].Dependencies[0].Wiring != nil {
		t.Fatalf("a refusal must return nothing and mutate nothing: %+v", design.Components[0])
	}
}

func TestDerivePlatformResourceFacts_AuthConflictRefuses(t *testing.T) {
	t.Parallel()
	design := assembled(t, designFilesWithDepsAndAuth(
		`[{"kind":"platform-resource","name":"user-auth","resourceType":"thunder-app"}]`,
		`{"auth": "service-required"}`))

	if _, err := DerivePlatformResourceFacts(design, thunderAndPostgres(), "web"); !errors.Is(err, ErrEndUserAuthConflict) {
		t.Fatalf("want ErrEndUserAuthConflict, got %v", err)
	}
}
