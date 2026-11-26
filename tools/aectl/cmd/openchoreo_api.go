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
	"fmt"
	"strconv"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// ocAPIServiceName is the Service OpenChoreo's control-plane chart gives its
// API (the default of openchoreoApi.name). aectl port-forwards to it in
// oc.system_namespace, as it does to Thunder's and OpenBao's fixed names.
const ocAPIServiceName = "openchoreo-api"

// ocAPIPort is the port of OpenChoreo's API Service to forward to: the one
// the chart names http. Its number is a chart value
// (openchoreoApi.service.port), so it is read from the Service.
func ocAPIPort(ctx context.Context, client kubernetes.Interface, namespace string) (string, error) {
	svc, err := client.CoreV1().Services(namespace).Get(ctx, ocAPIServiceName, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("read OpenChoreo's API Service %s/%s (oc.system_namespace): %w", namespace, ocAPIServiceName, err)
	}
	for _, p := range svc.Spec.Ports {
		if p.Name == "http" {
			return strconv.Itoa(int(p.Port)), nil
		}
	}
	return "", fmt.Errorf("OpenChoreo's API Service %s/%s has no port named http", namespace, ocAPIServiceName)
}
