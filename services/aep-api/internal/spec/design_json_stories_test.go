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
	"reflect"
	"strings"
	"testing"
)

// The stories field (#369, agent-authored during enrichment) must survive
// the strict design.json codec in BOTH directions: a decode-only fix would
// have the next save silently strip the field (the preflight regression that
// motivated this: `json: unknown field "stories"`).
func TestComponentDesignJSON_StoriesRoundTrip(t *testing.T) {
	raw := `{"name":"api","type":"service","language":"Go","buildpack":"docker","appPath":"api","entrypoint":"deployment/service","exposure":"intranet","stories":[1,2,4],"dependencies":[],"description":"d"}`
	comp, err := parseComponentDesignJSON("api", raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !reflect.DeepEqual(comp.Stories, []int{1, 2, 4}) {
		t.Fatalf("stories = %v", comp.Stories)
	}
	out, err := marshalComponentDesignJSON("api", comp)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if !strings.Contains(string(out), `"stories"`) {
		t.Fatalf("encode dropped stories: %s", out)
	}
}
