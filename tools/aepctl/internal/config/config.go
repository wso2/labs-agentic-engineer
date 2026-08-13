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

package config

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/viper"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// ConfigMapName is the in-cluster ConfigMap written by `aep init` and read by
// all subsequent commands. It lives in the AEP platform namespace (wso2-aep).
const ConfigMapName = "aep-cli-config"

// ConfigMapKeys is the canonical list of non-sensitive viper keys stored in
// ConfigMapName. thunder.admin_client_secret is intentionally absent — it is
// managed by OpenBao/ESO and read from the aep-thunder-secrets Secret instead.
var ConfigMapKeys = []string{
	"server",
	"thunder.namespace",
	"thunder.url",
	"thunder.config_map",
	"thunder.deployment",
	"thunder.admin_client_id",
	"thunder.public_url",
	"oc.api_url",
	"oc.org_namespace",
	"oc.local_org_provisioning.enabled",
	"platform.workspaces.access_mode",
	"codingagent.local_stubs.enabled",
	"codingagent.secret_manager_api.url",
	"webhook.delivery_url",
	"webhook.local_smee.enabled",
}

// Init sets viper defaults and env-var bindings. Config is no longer read from
// a local file — it is loaded from the cluster ConfigMap by LoadFromCluster,
// called in root's PersistentPreRunE for every command except `aep init`.
func Init() {
	viper.SetEnvPrefix("AEP")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv()

	// Platform install defaults.
	// platform.workspaces.access_mode: PVC access mode for the shared git workspaces
	// volume. Empty means use the chart default (ReadWriteMany).
	viper.SetDefault("platform.workspaces.access_mode", "")

	// OpenChoreo platform API defaults.
	viper.SetDefault("oc.api_url", "http://openchoreo-api.openchoreo-control-plane.svc.cluster.local:8080")

	// oc.local_org_provisioning.enabled: creates the per-org namespaced
	// ComponentTypes locally — only needed when no platform-api ProvisionOrgUnit
	// is running. Off by default in production.
	viper.SetDefault("oc.local_org_provisioning.enabled", false)
	viper.SetDefault("oc.org_namespace", "default")

	// Coding-agent dispatch.
	// codingagent.local_stubs.enabled: wires the local in-process secrets
	// delivery (OPENBAO_* on aep-api). Off by default in production; set
	// secret_manager_api.url to the real managed service URL instead.
	viper.SetDefault("codingagent.local_stubs.enabled", false)
	viper.SetDefault("codingagent.secret_manager_api.url", "")

	// GitHub webhook delivery.
	// webhook.delivery_url: registered on each repo's webhook. Set to the real
	// public HTTPS URL of aep-api's /api/v1/webhooks/github in production.
	// webhook.local_smee.enabled: deploys an in-cluster smee-client — off by
	// default in production; use a real ingress + public delivery_url instead.
	viper.SetDefault("webhook.delivery_url", "")
	viper.SetDefault("webhook.local_smee.enabled", false)

	// Thunder defaults — also overridable via AEP_THUNDER_* env vars.
	viper.SetDefault("thunder.namespace", "thunder")
	viper.SetDefault("thunder.url", "http://thunder-service.thunder.svc.cluster.local:8090")
	viper.SetDefault("thunder.config_map", "thunder-config-map")
	viper.SetDefault("thunder.deployment", "thunder-deployment")
	viper.SetDefault("thunder.admin_client_id", "openchoreo-system-app")
	viper.SetDefault("thunder.admin_client_secret", "openchoreo-system-app-secret")
	viper.SetDefault("thunder.public_url", "http://thunder.openchoreo.localhost:8080")
}

// LoadFromCluster reads the aep-cli-config ConfigMap from the given namespace
// and loads each entry into viper via SetDefault, so CLI flags and AEP_* env
// vars still take precedence. Returns nil if the ConfigMap does not yet exist
// (i.e. before `aep init` has run).
func LoadFromCluster(ctx context.Context, client *kubernetes.Clientset, namespace string) error {
	cm, err := client.CoreV1().ConfigMaps(namespace).Get(ctx, ConfigMapName, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("read %s ConfigMap: %w", ConfigMapName, err)
	}
	for k, v := range cm.Data {
		viper.SetDefault(k, v)
	}
	return nil
}

// LoadThunderSecretFromCluster reads the Thunder admin client secret from the
// ESO-synced aep-thunder-secrets Secret and sets it via viper.SetDefault so
// that CLI flags still override it. The secret is never stored in the ConfigMap.
// Returns nil if the Secret does not yet exist.
func LoadThunderSecretFromCluster(ctx context.Context, client *kubernetes.Clientset, namespace string) error {
	sec, err := client.CoreV1().Secrets(namespace).Get(ctx, "aep-thunder-secrets", metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("read aep-thunder-secrets: %w", err)
	}
	if v, ok := sec.Data["THUNDER_SYSTEM_CLIENT_SECRET"]; ok && len(v) > 0 {
		viper.SetDefault("thunder.admin_client_secret", string(v))
	}
	return nil
}
