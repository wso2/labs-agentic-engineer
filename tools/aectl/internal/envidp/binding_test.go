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

import (
	"context"
	"slices"
	"testing"
)

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

func TestBindingCredentialData(t *testing.T) {
	data := bindingCredentialData(&ThunderInstance{SystemClientSecret: "shh"})
	if len(data) != 2 || data["clientId"] != "aep-system-client" || data["clientSecret"] != "shh" {
		t.Fatalf("data = %v, want exactly {clientId: aep-system-client, clientSecret: shh}", data)
	}
}

func TestBindingAnnotations(t *testing.T) {
	cfg := Config{Org: "default", Env: "development"}
	inst := &ThunderInstance{PublicURL: "http://development-idp.openchoreo.localhost:8080", AdminURL: "http://admin", SystemResourceIdentifier: "sys"}
	got := bindingAnnotations(cfg, inst)
	want := []string{
		"aep.wso2.com/thunder-binding=thunder-binding-default-development",
		"aep.wso2.com/thunder-issuer=http://development-idp.openchoreo.localhost:8080",
		"aep.wso2.com/thunder-admin-url=http://admin",
		"aep.wso2.com/thunder-system-resource-identifier=sys",
		"aep.wso2.com/thunder-secret-ref=thunder-binding-default-development",
	}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("annotations = %v, want %v", got, want)
	}
}

type recordingBindingSecrets struct {
	namespace, name string
	data            map[string]string
	err             error
}

func (r *recordingBindingSecrets) PutSecret(_ context.Context, namespace, name string, data map[string]string) error {
	r.namespace, r.name, r.data = namespace, name, data
	return r.err
}

func TestWriteOpenChoreoBinding_StoresTheCredentialInTheOrgsNamespace(t *testing.T) {
	store := &recordingBindingSecrets{}
	cfg := Config{Org: "default", Env: "development", BindingSecrets: store}
	if err := writeOpenChoreoBinding(context.Background(), cfg, &ThunderInstance{SystemClientSecret: "shh"}); err != nil {
		t.Fatalf("writeOpenChoreoBinding: %v", err)
	}
	if store.namespace != "default" || store.name != "thunder-binding-default-development" || store.data["clientSecret"] != "shh" {
		t.Errorf("stored %s/%s %v", store.namespace, store.name, store.data)
	}
}

func TestWriteOpenChoreoBinding_RequiresAStore(t *testing.T) {
	if err := writeOpenChoreoBinding(context.Background(), Config{Org: "default", Env: "development"}, &ThunderInstance{}); err == nil {
		t.Fatal("want an error without Config.BindingSecrets")
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
