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

package envidp

import "testing"

func TestBindingName(t *testing.T) {
	if got, want := BindingName("default", "default"), "thunder-binding-default-default"; got != want {
		t.Errorf("BindingName = %q, want %q", got, want)
	}
}

func TestBindingLabels(t *testing.T) {
	labels := bindingLabels("acme", "prod")
	want := map[string]string{
		"aep.wso2.com/kind": "thunder-binding",
		"aep.wso2.com/org":  "acme",
		"aep.wso2.com/env":  "prod",
	}
	if len(labels) != len(want) {
		t.Fatalf("labels = %v, want %v", labels, want)
	}
	for k, v := range want {
		if labels[k] != v {
			t.Errorf("labels[%q] = %q, want %q", k, labels[k], v)
		}
	}
}

func TestBindingConfigMapData(t *testing.T) {
	inst := &ThunderInstance{
		PublicURL:                "http://default-idp.openchoreo.localhost:8080",
		AdminURL:                 "http://thunder-default-default-service.thunder-default-default.svc.cluster.local:8090",
		SystemResourceIdentifier: "http://default-idp.openchoreo.localhost:8080/mcp",
	}
	data := bindingConfigMapData("thunder-binding-default-default", inst)

	want := map[string]string{
		"issuer":                   inst.PublicURL,
		"adminURL":                 inst.AdminURL,
		"systemResourceIdentifier": inst.SystemResourceIdentifier,
		"secretName":               "thunder-binding-default-default",
		"secretNamespace":          operatorNamespace,
	}
	if len(data) != len(want) {
		t.Fatalf("data = %v, want %v", data, want)
	}
	for k, v := range want {
		if data[k] != v {
			t.Errorf("data[%q] = %q, want %q", k, data[k], v)
		}
	}
}

func TestBindingSecretData(t *testing.T) {
	inst := &ThunderInstance{SystemClientSecret: "shh"}
	data := bindingSecretData(inst)

	if string(data["client-id"]) != "aep-system-client" {
		t.Errorf("client-id = %q, want aep-system-client", data["client-id"])
	}
	if string(data["client-secret"]) != "shh" {
		t.Errorf("client-secret = %q, want shh", data["client-secret"])
	}
	if len(data) != 2 {
		t.Errorf("data has %d keys, want exactly 2 (client-id, client-secret)", len(data))
	}
}
