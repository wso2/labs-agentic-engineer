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
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// A provider document with more than a design would use: three paths, a
// schema chain (Charge → Customer → Address), an unrelated schema, two tags
// and a security scheme.
const sliceSource = `openapi: 3.0.3
info: { title: Payments, version: "2024-01" }
servers: [{ url: https://api.example.com/v1 }]
security: [{ apiKey: [] }]
tags:
  - { name: charges, description: Money in }
  - { name: refunds, description: Money out }
  - { name: webhooks }
paths:
  /charges:
    parameters: [{ name: idempotency-key, in: header, schema: { type: string } }]
    post:
      operationId: createCharge
      tags: [charges]
      requestBody:
        content: { application/json: { schema: { $ref: '#/components/schemas/ChargeRequest' } } }
      responses: { "201": { description: created, content: { application/json: { schema: { $ref: '#/components/schemas/Charge' } } } } }
    get:
      operationId: listCharges
      tags: [charges]
      responses: { "200": { description: ok } }
  /refunds:
    post:
      operationId: createRefund
      tags: [refunds]
      responses: { "201": { description: created, content: { application/json: { schema: { $ref: '#/components/schemas/Refund' } } } } }
  /webhooks:
    get:
      operationId: listWebhooks
      tags: [webhooks]
      responses: { "200": { description: ok } }
components:
  securitySchemes:
    apiKey: { type: http, scheme: bearer }
  schemas:
    ChargeRequest: { type: object, properties: { amount: { type: integer }, customer: { $ref: '#/components/schemas/Customer' } } }
    Charge: { type: object, properties: { id: { type: string }, customer: { $ref: '#/components/schemas/Customer' } } }
    Customer: { type: object, properties: { address: { $ref: '#/components/schemas/Address' } } }
    Address: { type: object, properties: { line1: { type: string } } }
    Refund: { type: object, properties: { id: { type: string } } }
    Unrelated: { type: object }
`

func sliceDoc(t *testing.T, selectors ...string) map[string]any {
	t.Helper()
	out, err := SliceOpenAPI([]byte(sliceSource), selectors)
	if err != nil {
		t.Fatalf("SliceOpenAPI(%v): %v", selectors, err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(out, &doc); err != nil {
		t.Fatalf("slice is not YAML: %v\n%s", err, out)
	}
	if _, verr := ValidateOpenAPI(out); verr != nil {
		t.Fatalf("slice is not a valid standalone document: %v\n%s", verr, out)
	}
	return doc
}

func keysOfMap(m any) []string {
	mm, _ := m.(map[string]any)
	out := make([]string, 0, len(mm))
	for k := range mm {
		out = append(out, k)
	}
	return out
}

func TestSliceOpenAPI_KeepsSelectedOperationsAndTheirSchemaClosure(t *testing.T) {
	doc := sliceDoc(t, "createCharge")
	paths := doc["paths"].(map[string]any)
	if len(paths) != 1 {
		t.Fatalf("paths = %v, want only /charges", keysOfMap(paths))
	}
	charges := paths["/charges"].(map[string]any)
	if _, ok := charges["post"]; !ok {
		t.Fatalf("post /charges dropped")
	}
	if _, ok := charges["get"]; ok {
		t.Fatalf("get /charges kept though not selected")
	}
	if _, ok := charges["parameters"]; !ok {
		t.Fatalf("path-level parameters must ride along with a kept method")
	}
	schemas := doc["components"].(map[string]any)["schemas"].(map[string]any)
	for _, want := range []string{"ChargeRequest", "Charge", "Customer", "Address"} {
		if _, ok := schemas[want]; !ok {
			t.Errorf("schema %s missing from the closure: %v", want, keysOfMap(schemas))
		}
	}
	for _, drop := range []string{"Refund", "Unrelated"} {
		if _, ok := schemas[drop]; ok {
			t.Errorf("schema %s kept though unreferenced", drop)
		}
	}
	// Security schemes always ride along; root info/servers/security too.
	if _, ok := doc["components"].(map[string]any)["securitySchemes"]; !ok {
		t.Errorf("securitySchemes dropped")
	}
	for _, k := range []string{"info", "servers", "security"} {
		if _, ok := doc[k]; !ok {
			t.Errorf("root %s dropped", k)
		}
	}
	// Only the tags a kept operation uses.
	tags := doc["tags"].([]any)
	if len(tags) != 1 || tags[0].(map[string]any)["name"] != "charges" {
		t.Errorf("tags = %v, want only charges", tags)
	}
}

func TestSliceOpenAPI_SelectorForms(t *testing.T) {
	// METHOD /path, case-insensitive; and a bare /path takes every method.
	doc := sliceDoc(t, "POST /refunds", "/charges")
	paths := doc["paths"].(map[string]any)
	if len(paths) != 2 {
		t.Fatalf("paths = %v", keysOfMap(paths))
	}
	charges := paths["/charges"].(map[string]any)
	if _, ok := charges["get"]; !ok {
		t.Fatalf("a bare /path keeps every method")
	}
	if _, ok := paths["/refunds"].(map[string]any)["post"]; !ok {
		t.Fatalf("METHOD /path selector dropped")
	}
}

func TestSliceOpenAPI_UnknownSelectorIsAnError(t *testing.T) {
	_, err := SliceOpenAPI([]byte(sliceSource), []string{"createCharge", "deleteEverything", "GET /nope"})
	if err == nil {
		t.Fatalf("want an error naming the unknown selectors")
	}
	for _, want := range []string{"deleteEverything", "GET /nope"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %q", err, want)
		}
	}
	if strings.Contains(err.Error(), "createCharge") {
		t.Errorf("error names a selector that resolved: %q", err)
	}
}

func TestSliceOpenAPI_RejectsNothingSelectedAndNonOpenAPI(t *testing.T) {
	if _, err := SliceOpenAPI([]byte(sliceSource), nil); err == nil {
		t.Fatalf("empty selection must be an error")
	}
	if _, err := SliceOpenAPI([]byte("swagger: '2.0'\npaths: {}\n"), []string{"/x"}); err == nil {
		t.Fatalf("a non-3.x document must be an error")
	}
	if _, err := SliceOpenAPI([]byte("{nope"), []string{"/x"}); err == nil {
		t.Fatalf("invalid YAML must be an error")
	}
}

func TestSliceOpenAPI_IsDeterministic(t *testing.T) {
	a, _ := SliceOpenAPI([]byte(sliceSource), []string{"createCharge", "createRefund"})
	b, _ := SliceOpenAPI([]byte(sliceSource), []string{"createRefund", "createCharge"})
	if string(a) != string(b) {
		t.Fatalf("selector order changed the output:\n%s\n---\n%s", a, b)
	}
}
