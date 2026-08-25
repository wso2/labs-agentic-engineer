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
	"testing"
)

func TestRunGatewayIngressCheck(t *testing.T) {
	tests := []struct {
		name    string
		deps    gatewayIngressDeps
		wantErr bool
	}{
		{
			name: "already configured",
			deps: gatewayIngressDeps{
				isConfigured: func(context.Context) (bool, error) { return true, nil },
			},
			wantErr: false,
		},
		{
			name: "not configured, user accepts defaults",
			deps: gatewayIngressDeps{
				isConfigured:    func(context.Context) (bool, error) { return false, nil },
				discoverGateway: func(context.Context) (string, string, error) { return "gateway-default", "openchoreo-data-plane", nil },
				prompt:          func(_, def string) (string, error) { return def, nil },
				confirm:         func(string) bool { return true },
				applyConfig:     func(context.Context, string, string, string) error { return nil },
			},
			wantErr: false,
		},
		{
			name: "not configured, user accepts custom hostname",
			deps: gatewayIngressDeps{
				isConfigured:    func(context.Context) (bool, error) { return false, nil },
				discoverGateway: func(context.Context) (string, string, error) { return "gateway-default", "openchoreo-data-plane", nil },
				prompt:          func(_, _ string) (string, error) { return "myapis.example.com", nil },
				confirm:         func(string) bool { return true },
				applyConfig: func(_ context.Context, gwName, gwNamespace, hostname string) error {
					if hostname != "myapis.example.com" {
						return fmt.Errorf("unexpected hostname: %s", hostname)
					}
					return nil
				},
			},
			wantErr: false,
		},
		{
			name: "not configured, user declines",
			deps: gatewayIngressDeps{
				isConfigured:    func(context.Context) (bool, error) { return false, nil },
				discoverGateway: func(context.Context) (string, string, error) { return "gateway-default", "openchoreo-data-plane", nil },
				prompt:          func(_, def string) (string, error) { return def, nil },
				confirm:         func(string) bool { return false },
			},
			wantErr: true,
		},
		{
			name: "check error",
			deps: gatewayIngressDeps{
				isConfigured: func(context.Context) (bool, error) { return false, fmt.Errorf("connection refused") },
			},
			wantErr: true,
		},
		{
			name: "apply config fails",
			deps: gatewayIngressDeps{
				isConfigured:    func(context.Context) (bool, error) { return false, nil },
				discoverGateway: func(context.Context) (string, string, error) { return "gateway-default", "openchoreo-data-plane", nil },
				prompt:          func(_, def string) (string, error) { return def, nil },
				confirm:         func(string) bool { return true },
				applyConfig:     func(context.Context, string, string, string) error { return fmt.Errorf("kubectl error") },
			},
			wantErr: true,
		},
		{
			// Regression: isGatewayIngressConfigured returns false when only
			// ClusterDataPlane is configured but Environment/development is not.
			// The install flow must still call applyConfig to patch both resources.
			name: "partial config (ClusterDataPlane ok, Environment missing) triggers reconfigure",
			deps: gatewayIngressDeps{
				isConfigured:    func(context.Context) (bool, error) { return false, nil },
				discoverGateway: func(context.Context) (string, string, error) { return "gateway-default", "openchoreo-data-plane", nil },
				prompt:          func(_, def string) (string, error) { return def, nil },
				confirm:         func(string) bool { return true },
				applyConfig:     func(context.Context, string, string, string) error { return nil },
			},
			wantErr: false,
		},
		{
			// Regression: a kubectl error from discoverGateway must propagate
			// rather than silently falling back to "gateway-default".
			name: "gateway discovery error propagates",
			deps: gatewayIngressDeps{
				isConfigured:    func(context.Context) (bool, error) { return false, nil },
				discoverGateway: func(context.Context) (string, string, error) { return "", "", fmt.Errorf("gateway CRD not registered") },
				prompt:          func(_, def string) (string, error) { return def, nil },
			},
			wantErr: true,
		},
		{
			// CI path: hostnameOverride bypasses prompt and confirm entirely.
			// prompt and confirm are nil — a call to either would panic.
			name: "hostname override auto-configures without prompting",
			deps: gatewayIngressDeps{
				isConfigured:     func(context.Context) (bool, error) { return false, nil },
				discoverGateway:  func(context.Context) (string, string, error) { return "gateway-default", "openchoreo-data-plane", nil },
				hostnameOverride: "myapis.example.com",
				applyConfig: func(_ context.Context, gwName, gwNamespace, hostname string) error {
					if gwName != "gateway-default" {
						return fmt.Errorf("unexpected gwName: %s", gwName)
					}
					if gwNamespace != "openchoreo-data-plane" {
						return fmt.Errorf("unexpected gwNamespace: %s", gwNamespace)
					}
					if hostname != "myapis.example.com" {
						return fmt.Errorf("unexpected hostname: %s", hostname)
					}
					return nil
				},
			},
			wantErr: false,
		},
		{
			// CI path: an applyConfig error must propagate even with override set.
			name: "hostname override propagates apply error",
			deps: gatewayIngressDeps{
				isConfigured:     func(context.Context) (bool, error) { return false, nil },
				discoverGateway:  func(context.Context) (string, string, error) { return "gateway-default", "openchoreo-data-plane", nil },
				hostnameOverride: "myapis.example.com",
				applyConfig:      func(context.Context, string, string, string) error { return fmt.Errorf("kubectl error") },
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := runGatewayIngressCheck(context.Background(), tt.deps)
			if (err != nil) != tt.wantErr {
				t.Errorf("runGatewayIngressCheck() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestRunGatewayIngressCheck_HostnameOverride_GatewayForwarding verifies that
// in the CI override path discoverGateway is actually invoked and that both the
// gateway name and namespace it returns are forwarded unchanged to applyConfig.
func TestRunGatewayIngressCheck_HostnameOverride_GatewayForwarding(t *testing.T) {
	discoverCalled := false
	var capturedGwName, capturedGwNs string

	deps := gatewayIngressDeps{
		isConfigured: func(context.Context) (bool, error) { return false, nil },
		discoverGateway: func(context.Context) (string, string, error) {
			discoverCalled = true
			return "gateway-default", "openchoreo-data-plane", nil
		},
		hostnameOverride: "myapis.example.com",
		applyConfig: func(_ context.Context, gwName, gwNamespace, _ string) error {
			capturedGwName = gwName
			capturedGwNs = gwNamespace
			return nil
		},
	}

	if err := runGatewayIngressCheck(context.Background(), deps); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !discoverCalled {
		t.Error("discoverGateway must be called in the CI override path")
	}
	if capturedGwName != "gateway-default" {
		t.Errorf("applyConfig received gwName = %q, want %q", capturedGwName, "gateway-default")
	}
	if capturedGwNs != "openchoreo-data-plane" {
		t.Errorf("applyConfig received gwNamespace = %q, want %q", capturedGwNs, "openchoreo-data-plane")
	}
}
