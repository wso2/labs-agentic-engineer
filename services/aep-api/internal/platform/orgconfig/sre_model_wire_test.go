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

package orgconfig_test

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/platform/orgconfig"
)

// The sreLlm section is patched field by field: an absent field stays nil,
// so the service keeps what is stored.
func TestSreLlmWrite_AbsentFieldsStayNil(t *testing.T) {
	t.Parallel()
	var p orgconfig.ConfigPatch
	if err := json.Unmarshal([]byte(`{"sreLlm":{"model":"gpt-4o"}}`), &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !p.SreLLM.Sent || p.SreLLM.Null {
		t.Fatalf("sreLlm: Sent=%v Null=%v, want a value", p.SreLLM.Sent, p.SreLLM.Null)
	}
	w := p.SreLLM.Value
	if w.Model == nil || *w.Model != "gpt-4o" || w.BaseURL != nil || w.APIKey != nil {
		t.Fatalf("sreLlm value = {BaseURL:%v APIKey set:%v Model:%v}, want only model", w.BaseURL, w.APIKey != nil, w.Model)
	}
}

func TestSreLlmWrite_NullClears(t *testing.T) {
	t.Parallel()
	var p orgconfig.ConfigPatch
	if err := json.Unmarshal([]byte(`{"sreLlm":null}`), &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !p.SreLLM.Sent || !p.SreLLM.Null {
		t.Fatalf("sreLlm null: Sent=%v Null=%v, want both true", p.SreLLM.Sent, p.SreLLM.Null)
	}
}

// Both sections are required-nullable on the wire: always present, null when
// unset.
func TestConfigProjection_SreSectionsMarshalAsNull(t *testing.T) {
	t.Parallel()
	raw, err := json.Marshal(orgconfig.ConfigProjection{})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, key := range []string{"sreLlm", "sreAgent"} {
		v, ok := m[key]
		if !ok || v != nil {
			t.Errorf("%s = %v (present=%v), want a null key", key, v, ok)
		}
	}
}

func TestSreAgentEnumTagsMatchTheContract(t *testing.T) {
	typ := reflect.TypeOf(orgconfig.SreAgentProjection{})
	for field, property := range map[string]string{"Source": "source", "Status": "status"} {
		f, _ := typ.FieldByName(field)
		got := strings.Split(f.Tag.Get("enum"), ",")
		if want := propertyEnumFromContract(t, "SreAgentProjection", property); !slices.Equal(got, want) {
			t.Errorf("SreAgentProjection.%s enum tag = %v, contract enum = %v", field, got, want)
		}
	}
}
