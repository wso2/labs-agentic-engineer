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

package taskplan

import (
	"encoding/json"
	"testing"
)

func TestToolResultDecoders(t *testing.T) {
	planOk := `{"ok":true,"op":"plan","component":"svc","title":"Do it","dependsOn":["a"],"origin":"spec-plan","rationale":"why"}`
	p, err := DecodePlanTaskOk(json.RawMessage(planOk))
	if err != nil {
		t.Fatalf("DecodePlanTaskOk: %v", err)
	}
	if p.Component != "svc" || p.Origin != OriginSpecPlan {
		t.Fatalf("planOk fields: %+v", p)
	}

	updateOk := `{"ok":true,"op":"update","ref":{"title":"Do it"},"set":{"body":"b"}}`
	u, err := DecodeUpdateTaskOk(json.RawMessage(updateOk))
	if err != nil {
		t.Fatalf("DecodeUpdateTaskOk: %v", err)
	}
	if u.Ref.Title == nil || *u.Ref.Title != "Do it" {
		t.Fatalf("updateOk ref: %+v", u.Ref)
	}
	// An omitted set field decodes to a nil pointer (leave-unchanged semantics).
	if u.Set.Title != nil || u.Set.DependsOn != nil || u.Set.Rationale != nil {
		t.Fatalf("omitted set fields should be nil: %+v", u.Set)
	}

	// A plan result cannot be decoded as an update result and vice versa.
	if _, err := DecodeUpdateTaskOk(json.RawMessage(planOk)); err == nil {
		t.Fatalf("expected op mismatch error decoding plan as update")
	}

	// The resolved ref union is enforced (exactly one of issueNumber/title).
	if _, err := DecodeUpdateTaskOk(json.RawMessage(`{"ok":true,"op":"update","ref":{"issueNumber":1,"title":"x"},"set":{}}`)); err == nil {
		t.Fatalf("expected error for both ref branches")
	}
	if _, err := DecodeUpdateTaskOk(json.RawMessage(`{"ok":true,"op":"update","ref":{},"set":{}}`)); err == nil {
		t.Fatalf("expected error for neither ref branch")
	}

	// A failed tool result (ok:false, self-correction) is never a success.
	toolErr := `{"ok":false,"op":"plan","code":"UNKNOWN_COMPONENT","message":"no such component"}`
	if _, err := DecodePlanTaskOk(json.RawMessage(toolErr)); err == nil {
		t.Fatalf("expected an ok:false result to be refused")
	}
}
