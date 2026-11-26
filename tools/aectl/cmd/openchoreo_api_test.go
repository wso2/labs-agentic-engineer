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

package cmd

import (
	"context"
	"strings"
	"testing"

	"github.com/spf13/viper"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/wso2/aep/aectl/internal/openchoreo"
)

func ocAPIServiceWithPorts(namespace string, ports ...corev1.ServicePort) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "openchoreo-api", Namespace: namespace},
		Spec:       corev1.ServiceSpec{Ports: ports},
	}
}

// aectl reaches OpenChoreo's API through a port-forward to the chart's fixed
// Service, openchoreo-api, in OpenChoreo's namespace, on the port named http.
func TestOCAPIPort_IsTheServicesHTTPPort(t *testing.T) {
	c := fake.NewSimpleClientset(ocAPIServiceWithPorts("openchoreo-control-plane",
		corev1.ServicePort{Name: "metrics", Port: 9090},
		corev1.ServicePort{Name: "http", Port: 8081},
	))
	port, err := ocAPIPort(context.Background(), c, "openchoreo-control-plane")
	if err != nil || port != "8081" {
		t.Fatalf("ocAPIPort = (%q, %v), want 8081 (the port named http)", port, err)
	}
}

func TestOCAPIPort_MissingServiceNamesWhereItLooked(t *testing.T) {
	c := fake.NewSimpleClientset()
	_, err := ocAPIPort(context.Background(), c, "openchoreo-control-plane")
	if err == nil || !strings.Contains(err.Error(), "openchoreo-control-plane/openchoreo-api") {
		t.Fatalf("err = %v, want one naming openchoreo-control-plane/openchoreo-api", err)
	}
}

func TestAectlClientSecret(t *testing.T) {
	withSecret := func(data map[string][]byte) *fake.Clientset {
		return fake.NewSimpleClientset(&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: aectlClientSecretsName, Namespace: "wso2-aep"},
			Data:       data,
		})
	}
	ctx := context.Background()

	got, err := aectlClientSecret(ctx, withSecret(map[string][]byte{"AECTL_OPENCHOREO_CLIENT_SECRET": []byte("s3cret")}), "wso2-aep")
	if err != nil || got != "s3cret" {
		t.Fatalf("aectlClientSecret = (%q, %v), want s3cret", got, err)
	}
	if _, err := aectlClientSecret(ctx, fake.NewSimpleClientset(), "wso2-aep"); err == nil || !strings.Contains(err.Error(), aectlClientSecretsName) {
		t.Errorf("missing Secret: err = %v, want one naming %s", err, aectlClientSecretsName)
	}
	if _, err := aectlClientSecret(ctx, withSecret(map[string][]byte{"AECTL_OPENCHOREO_CLIENT_SECRET": nil}), "wso2-aep"); err == nil {
		t.Error("an empty secret was accepted")
	}
}

func TestSecretTargetPlane(t *testing.T) {
	t.Cleanup(viper.Reset)
	if got := secretTargetPlane(); got != (openchoreo.TargetPlane{Kind: "ClusterDataPlane", Name: "default"}) {
		t.Errorf("unset = %+v, want ClusterDataPlane/default", got)
	}
	viper.Set("oc.secret_target_plane.kind", "DataPlane")
	viper.Set("oc.secret_target_plane.name", "edge")
	if got := secretTargetPlane(); got != (openchoreo.TargetPlane{Kind: "DataPlane", Name: "edge"}) {
		t.Errorf("set = %+v, want DataPlane/edge", got)
	}
}

func TestOCAPIPort_NoHTTPPortIsAnError(t *testing.T) {
	c := fake.NewSimpleClientset(ocAPIServiceWithPorts("openchoreo-control-plane", corev1.ServicePort{Name: "grpc", Port: 9000}))
	if port, err := ocAPIPort(context.Background(), c, "openchoreo-control-plane"); err == nil {
		t.Fatalf("ocAPIPort = %q, want an error: the Service has no port named http", port)
	}
}
