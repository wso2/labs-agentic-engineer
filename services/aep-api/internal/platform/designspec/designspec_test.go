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

package designspec

import (
	"errors"
	"testing"
)

const validComponent = `{
  "name": "orders-service",
  "type": "service",
  "version": "1.0",
  "language": "go",
  "buildpack": "go",
  "appPath": ".",
  "entrypoint": "main.go",
  "exposure": "intranet",
  "dependencies": [{"kind": "component", "name": "orders-db"}],
  "description": "Handles the order lifecycle"
}`

func wantCode(t *testing.T, err error, code string) {
	t.Helper()
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("want *ValidationError, got %v", err)
	}
	if ve.Code != code {
		t.Fatalf("code = %q, want %q (%s)", ve.Code, code, ve.Message)
	}
}

func TestValidComponentDesign(t *testing.T) {
	if err := ValidateComponentDesign([]byte(validComponent)); err != nil {
		t.Fatalf("valid component rejected: %v", err)
	}
}

func TestInvalidJSON(t *testing.T) {
	wantCode(t, ValidateComponentDesign([]byte("{ not json")), CodeInvalidJSON)
}

func TestMissingRequired(t *testing.T) {
	// Drop the required "description".
	raw := `{"name":"x","type":"service","version":"1","language":"go","buildpack":"go","appPath":".","entrypoint":"m","exposure":"intranet","dependencies":[]}`
	wantCode(t, ValidateComponentDesign([]byte(raw)), CodeSchemaViolation)
}

func TestBadEnum(t *testing.T) {
	raw := `{"name":"x","type":"service","version":"1","language":"go","buildpack":"go","appPath":".","entrypoint":"m","exposure":"public","dependencies":[],"description":"d"}`
	wantCode(t, ValidateComponentDesign([]byte(raw)), CodeSchemaViolation)
}

func TestAdditionalProperty(t *testing.T) {
	raw := `{"name":"x","type":"service","version":"1","language":"go","buildpack":"go","appPath":".","entrypoint":"m","exposure":"intranet","dependencies":[],"description":"d","surprise":true}`
	wantCode(t, ValidateComponentDesign([]byte(raw)), CodeSchemaViolation)
}

func TestDependencyMissingKind(t *testing.T) {
	// A dependency entry missing the required "kind" → SCHEMA_VIOLATION.
	raw := `{"name":"x","type":"service","version":"1","language":"go","buildpack":"go","appPath":".","entrypoint":"m","exposure":"intranet","dependencies":[{"name":"db"}],"description":"d"}`
	wantCode(t, ValidateComponentDesign([]byte(raw)), CodeSchemaViolation)
}

func TestNameMustEqualDir(t *testing.T) {
	if err := ValidateComponentDesignInDir([]byte(validComponent), "orders-service"); err != nil {
		t.Fatalf("matching dir rejected: %v", err)
	}
	wantCode(t, ValidateComponentDesignInDir([]byte(validComponent), "payments"), CodeSchemaViolation)
}

// --- an external dependency is a reference; its definition has its own file --
//
// The published component schema no longer declares style/package/specPath/
// candidates/config on a dependency (additionalProperties: false rejects
// them); the definition is dependency-design.schema.json, validated by
// ValidateDependencyDesignInDir below.

func designWithDep(depJSON string) string {
	return `{"name":"x","type":"service","version":"1","language":"go","buildpack":"go","appPath":".","entrypoint":"m","exposure":"intranet","description":"d","dependencies":[` + depJSON + `]}`
}

func TestExternalReferenceAccepted(t *testing.T) {
	dep := `{"kind":"external","name":"stripe","description":"charges shipping"}`
	if err := ValidateComponentDesign([]byte(designWithDep(dep))); err != nil {
		t.Fatalf("bare external reference rejected: %v", err)
	}
}

func TestMovedDependencyFieldsRejected(t *testing.T) {
	for _, dep := range []string{
		`{"kind":"external","name":"stripe","style":"sdk"}`,
		`{"kind":"external","name":"stripe","package":"npm:stripe@^14"}`,
		`{"kind":"external","name":"stripe","specPath":"dependencies/stripe.openapi.yaml"}`,
		`{"kind":"external","name":"email","candidates":[{"name":"a","style":"rest-api"},{"name":"b","style":"sdk"}]}`,
		`{"kind":"external","name":"stripe","config":[{"key":"STRIPE_API_KEY","secret":true}]}`,
	} {
		wantCode(t, ValidateComponentDesign([]byte(designWithDep(dep))), CodeSchemaViolation)
	}
}

const validDependency = `{"name":"payment-provider","description":"Charges shipping.","provider":"Stripe","style":"rest-api","contract":"openapi.yaml","config":[{"key":"PAYMENT_API_KEY","secret":true}]}`

func TestDependencyDesign_AcceptsTheDefinitionShape(t *testing.T) {
	if err := ValidateDependencyDesignInDir([]byte(validDependency), "payment-provider"); err != nil {
		t.Fatalf("valid definition rejected: %v", err)
	}
	stub := `{"name":"payment-provider","source":"org"}`
	if err := ValidateDependencyDesignInDir([]byte(stub), "payment-provider"); err != nil {
		t.Fatalf("registered-org stub rejected: %v", err)
	}
	open := `{"name":"email","suggestions":[{"name":"a","style":"rest-api"},{"name":"b"}]}`
	if err := ValidateDependencyDesignInDir([]byte(open), "email"); err != nil {
		t.Fatalf("open suggestions rejected: %v", err)
	}
	one := `{"name":"email","suggestions":[{"name":"a"}]}`
	if err := ValidateDependencyDesignInDir([]byte(one), "email"); err != nil {
		t.Fatalf("a single suggestion rejected: %v", err)
	}
}

func TestDependencyDesign_RejectsUnknownKeysStateAndDirMismatch(t *testing.T) {
	wantCode(t, ValidateDependencyDesignInDir([]byte(`{nope`), "payment-provider"), CodeInvalidJSON)
	wantCode(t, ValidateDependencyDesignInDir([]byte(`{"name":"payment-provider","status":"resolved"}`), "payment-provider"), CodeSchemaViolation)
	wantCode(t, ValidateDependencyDesignInDir([]byte(`{"name":"payment-provider","specPath":"x"}`), "payment-provider"), CodeSchemaViolation)
	wantCode(t, ValidateDependencyDesignInDir([]byte(`{"name":"payment-provider","style":"soap"}`), "payment-provider"), CodeSchemaViolation)
	wantCode(t, ValidateDependencyDesignInDir([]byte(`{"name":"email","candidates":[{"name":"a","style":"rest-api"},{"name":"b","style":"sdk"}]}`), "email"), CodeSchemaViolation)
	wantCode(t, ValidateDependencyDesignInDir([]byte(validDependency), "stripe"), CodeSchemaViolation)
}

func TestSuggestions_NameRequiredStyleOptional(t *testing.T) {
	wantCode(t, ValidateDependencyDesignInDir([]byte(`{"name":"email","suggestions":[{"style":"rest-api"}]}`), "email"), CodeSchemaViolation)
	wantCode(t, ValidateDependencyDesignInDir([]byte(`{"name":"email","suggestions":[{"name":"a","package":"npm:a"}]}`), "email"), CodeSchemaViolation)
}

// TestRetiredExternalFieldsRejected documents the hard-break: specUrl (URL
// hint) and sources (provenance array) were removed from the schema — the
// coding agent now researches contracts freely from the web — so both reject
// the same way any other unknown dependency property does
// (additionalProperties: false).
func TestRetiredExternalFieldsRejected(t *testing.T) {
	for _, dep := range []string{
		`{"kind":"external","name":"stripe","specUrl":"https://example.com/stripe.yaml"}`,
		`{"kind":"external","name":"stripe","sources":["https://stripe.com/docs/api"]}`,
	} {
		wantCode(t, ValidateComponentDesign([]byte(designWithDep(dep))), CodeSchemaViolation)
	}
}

// TestNeedsSpecNoLongerKnown documents the hard-break: needsSpec was removed
// from the schema entirely, so it is now rejected the same way any other
// unknown dependency property is (additionalProperties: false).
func TestNeedsSpecNoLongerKnown(t *testing.T) {
	dep := `{"kind":"external","name":"stripe","needsSpec":true}`
	wantCode(t, ValidateComponentDesign([]byte(designWithDep(dep))), CodeSchemaViolation)
}
