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

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
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

func TestOCAPIPort_NoHTTPPortIsAnError(t *testing.T) {
	c := fake.NewSimpleClientset(ocAPIServiceWithPorts("openchoreo-control-plane", corev1.ServicePort{Name: "grpc", Port: 9000}))
	if port, err := ocAPIPort(context.Background(), c, "openchoreo-control-plane"); err == nil {
		t.Fatalf("ocAPIPort = %q, want an error: the Service has no port named http", port)
	}
}
