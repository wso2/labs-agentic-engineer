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
	"net/url"
	"strconv"
	"strings"

	"github.com/spf13/viper"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// ConfigMapName is the in-cluster ConfigMap written by `aep init` and read by
// all subsequent commands. It lives in the AEP platform namespace (wso2-aep).
const ConfigMapName = "aep-cli-config"

// ThunderOperatorCredsSecret is the ESO-synced Secret that holds the Thunder
// system client credentials used by the thunder-app-operator.
const ThunderOperatorCredsSecret = "aep-thunder-operator-creds"

// ThunderOperatorCredsSecretKey is the key within ThunderOperatorCredsSecret
// that holds the OAuth client secret.
const ThunderOperatorCredsSecretKey = "client-secret"

// ConfigMapKeys is the canonical list of non-sensitive viper keys stored in
// ConfigMapName. thunder.admin_client_secret is intentionally absent — it is
// managed by OpenBao/ESO and read from the aep-thunder-secrets Secret instead.
var ConfigMapKeys = []string{
	"thunder.namespace",
	"thunder.url",
	"thunder.config_map",
	"thunder.deployment",
	"thunder.admin_client_id",
	"thunder.public_url",
	"oc.api_url",
	"oc.system_namespace",
	"oc.org_namespace",
	"oc.local_org_provisioning.enabled",
	"platform.workspaces.access_mode",
	"codingagent.openbao_direct.enabled",
	"openbao.addr",
	"webhook.delivery_url",
	"webhook.local_smee.enabled",
}

// keyKind describes the expected type of a config value for validation.
type keyKind int

const (
	kindString keyKind = iota
	kindBool
	kindURL
	kindEnum
)

type configKeyMeta struct {
	required   bool
	kind       keyKind
	enumValues []string // only for kindEnum
}

// keyRegistry maps each ConfigMapKey to its validation metadata.
var keyRegistry = map[string]configKeyMeta{
	"thunder.namespace":                 {required: true, kind: kindString},
	"thunder.url":                       {required: true, kind: kindURL},
	"thunder.config_map":                {required: true, kind: kindString},
	"thunder.deployment":                {required: true, kind: kindString},
	"thunder.admin_client_id":           {required: true, kind: kindString},
	"thunder.public_url":                {required: true, kind: kindURL},
	"oc.api_url":                        {required: true, kind: kindURL},
	"oc.system_namespace":               {required: true, kind: kindString},
	"oc.org_namespace":                  {required: false, kind: kindString},
	"oc.local_org_provisioning.enabled": {required: false, kind: kindBool},
	"platform.workspaces.access_mode":   {required: false, kind: kindEnum, enumValues: []string{"", "ReadWriteOnce", "ReadWriteMany", "ReadOnlyMany"}},
	"codingagent.openbao_direct.enabled": {required: false, kind: kindBool},
	"openbao.addr":                      {required: false, kind: kindURL},
	"webhook.delivery_url":              {required: false, kind: kindURL},
	"webhook.local_smee.enabled":        {required: false, kind: kindBool},
}

// Init sets env-var bindings. All config values must come from the cluster
// ConfigMap loaded by LoadFromCluster — no hardcoded defaults are set here.
func Init() {
	viper.SetEnvPrefix("AEP")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv()
}

// ValidateFile reads the YAML at path into an isolated viper instance and
// returns one error string per invalid or missing required key. Empty slice
// means the file is valid.
func ValidateFile(path string) []string {
	fv := viper.New()
	fv.SetConfigFile(path)
	if err := fv.ReadInConfig(); err != nil {
		return []string{fmt.Sprintf("cannot read config file: %s", err)}
	}
	return validateViper(fv)
}

// ValidateLoaded checks the currently resolved global viper state (populated
// after LoadFromCluster) and returns one error string per invalid or missing
// required key.
func ValidateLoaded() []string {
	return validateViper(viper.GetViper())
}

func validateViper(v *viper.Viper) []string {
	var errs []string
	for _, k := range ConfigMapKeys {
		meta, ok := keyRegistry[k]
		if !ok {
			continue
		}
		val := v.GetString(k)
		if meta.required && val == "" {
			errs = append(errs, fmt.Sprintf("%s: required but not set", k))
			continue
		}
		if val == "" {
			continue // optional + empty → skip further checks
		}
		switch meta.kind {
		case kindBool:
			if _, err := strconv.ParseBool(val); err != nil {
				errs = append(errs, fmt.Sprintf("%s: invalid boolean value %q", k, val))
			}
		case kindURL:
			parsed, err := url.ParseRequestURI(val)
			if err != nil || parsed.Host == "" {
				errs = append(errs, fmt.Sprintf("%s: not a valid absolute URL (must include scheme and host): %q", k, val))
			}
		case kindEnum:
			valid := false
			for _, ev := range meta.enumValues {
				if val == ev {
					valid = true
					break
				}
			}
			if !valid {
				errs = append(errs, fmt.Sprintf("%s: invalid value %q — must be one of %v", k, val, meta.enumValues))
			}
		}
	}
	return errs
}

// LoadFromCluster reads the aep-cli-config ConfigMap from the given namespace
// and loads each entry into viper via SetDefault, so CLI flags and AEP_* env
// vars still take precedence. Returns the number of keys loaded and nil if the
// ConfigMap does not yet exist (i.e. before `aep init` has run).
func LoadFromCluster(ctx context.Context, client kubernetes.Interface, namespace string) (int, error) {
	cm, err := client.CoreV1().ConfigMaps(namespace).Get(ctx, ConfigMapName, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("read %s ConfigMap: %w", ConfigMapName, err)
	}
	for k, v := range cm.Data {
		viper.SetDefault(k, v)
	}
	return len(cm.Data), nil
}

// LoadThunderSecretFromCluster reads the Thunder admin client secret from the
// ESO-synced ThunderOperatorCredsSecret and sets it via viper.SetDefault so
// that AEP_THUNDER_ADMIN_CLIENT_SECRET env and the interactive prompt still
// take precedence. The secret is never stored in the ConfigMap.
// Returns nil if the Secret does not yet exist (first install).
func LoadThunderSecretFromCluster(ctx context.Context, client kubernetes.Interface, namespace string) error {
	sec, err := client.CoreV1().Secrets(namespace).Get(ctx, ThunderOperatorCredsSecret, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("read %s: %w", ThunderOperatorCredsSecret, err)
	}
	v, ok := sec.Data[ThunderOperatorCredsSecretKey]
	if !ok || len(v) == 0 {
		return fmt.Errorf("%s is missing non-empty key %q — ESO sync may be incomplete", ThunderOperatorCredsSecret, ThunderOperatorCredsSecretKey)
	}
	viper.SetDefault("thunder.admin_client_secret", string(v))
	return nil
}
