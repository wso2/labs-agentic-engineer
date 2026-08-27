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

package provisioning

import "testing"

func TestMemoryValuePlane_CellsAreOrgScoped(t *testing.T) {
	t.Parallel()
	p := NewMemoryValuePlane()
	p.PutEnvCells("acme", "stripe", []EnvCell{
		{Environment: "development", Key: "region", Status: "configured", Value: "us"},
	})
	if got := p.EnvCells("acme", "stripe"); len(got) != 1 || got[0].Value != "us" {
		t.Fatalf("acme stripe cells = %#v", got)
	}
	if got := p.EnvCells("other", "stripe"); len(got) != 0 {
		t.Fatalf("EnvCells(other, stripe) = %#v, want empty", got)
	}
	if got := p.Instances("other", "stripe"); len(got) != 0 {
		t.Fatalf("Instances(other, stripe) = %#v, want empty", got)
	}
}
