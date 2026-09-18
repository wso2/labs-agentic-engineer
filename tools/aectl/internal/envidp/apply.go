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
	"net/url"

	corev1 "k8s.io/api/core/v1"
	applyv1 "k8s.io/client-go/applyconfigurations/core/v1"
)

// applyConfigMap builds a server-side-apply ConfigMap configuration. Used
// instead of Create/Update so a re-run of this package converges an existing
// ConfigMap's contents rather than erroring on AlreadyExists.
func applyConfigMap(namespace, name string, data, labels map[string]string) *applyv1.ConfigMapApplyConfiguration {
	return applyv1.ConfigMap(name, namespace).WithData(data).WithLabels(labels)
}

// applySecret builds a server-side-apply Secret configuration, same
// converge-don't-error rationale as applyConfigMap.
func applySecret(namespace, name string, data map[string][]byte, labels map[string]string) *applyv1.SecretApplyConfiguration {
	return applyv1.Secret(name, namespace).WithType(corev1.SecretTypeOpaque).WithData(data).WithLabels(labels)
}

// hostnameOf extracts the host (no port) from a "http://host:port" URL, for
// building an httproute.hostnames[] value from a public URL.
func hostnameOf(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	return u.Hostname()
}
