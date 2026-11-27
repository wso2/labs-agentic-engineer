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

package thunder

import (
	"context"
	"time"

	"github.com/wso2/aep/aectl/internal/kubernetes"
)

// remotePort is Thunder's admin service port inside the cluster.
const remotePort = "8090"

// PortForwardHandle is a running kubectl port-forward process.
type PortForwardHandle = kubernetes.PortForwardHandle

// PortForward starts kubectl port-forward to svc/thunder-service (the
// platform IdP's fixed service name) in the given namespace on an ephemeral
// local port. Caller must call Stop() when done.
func PortForward(ctx context.Context, namespace, kubeconfig string) (*PortForwardHandle, error) {
	return PortForwardService(ctx, namespace, "thunder-service", kubeconfig)
}

// PortForwardService starts kubectl port-forward to svc/<service> on
// Thunder's admin port in the given namespace on an ephemeral local port.
// Unlike PortForward, the service name is a parameter — an environment (T2)
// Thunder's release name (and so its Service name) varies per (org,
// environment), unlike the platform IdP's fixed "thunder-service". Caller
// must call Stop() when done.
func PortForwardService(ctx context.Context, namespace, service, kubeconfig string) (*PortForwardHandle, error) {
	return kubernetes.PortForwardService(ctx, namespace, service, remotePort, kubeconfig)
}

// WaitForReachable retries GET /oauth2/jwks until Thunder responds (any HTTP
// status) or timeout expires. It returns early if the port-forward process
// exits before Thunder becomes reachable, including any captured stderr output.
func WaitForReachable(ctx context.Context, baseURL string, timeout time.Duration, pf *PortForwardHandle) error {
	return kubernetes.WaitForReachable(ctx, baseURL+"/oauth2/jwks", timeout, pf)
}
