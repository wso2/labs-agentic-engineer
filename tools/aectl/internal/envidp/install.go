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
	"fmt"

	"k8s.io/client-go/kubernetes"
)

// Install installs this environment's T2 Thunder, writes its binding record,
// and installs the environment's API Platform gateway wired to that binding —
// in that order, since the gateway's ThunderKeyManager and the binding
// record's contents both depend on Thunder already answering.
//
// Not configured by this function: the HTTPS trust chain that lets T2 fetch
// the platform IdP's JWKS as a trusted issuer (custom CA distribution, DNS
// resolution of the platform IdP's public hostname from inside the cluster).
// That is cluster/environment-specific infrastructure this package assumes
// is already in place — see the package doc comment.
func Install(ctx context.Context, k8sClient kubernetes.Interface, cfg Config) error {
	if cfg.Org == "" || cfg.Env == "" {
		return fmt.Errorf("envidp: Org and Env must both be set")
	}
	if err := validReleaseName(releaseName(cfg.Org, cfg.Env)); err != nil {
		return fmt.Errorf("envidp: %w", err)
	}
	c := clients{k8s: k8sClient, kubeconfig: cfg.Kubeconfig}

	inst, err := installThunder(ctx, c, cfg)
	if err != nil {
		return fmt.Errorf("install environment Thunder: %w", err)
	}

	if err := writeBindingRecord(ctx, c, cfg, inst); err != nil {
		return fmt.Errorf("write Thunder binding record: %w", err)
	}

	if err := installGateway(ctx, c, cfg, inst); err != nil {
		return fmt.Errorf("install environment gateway: %w", err)
	}

	return nil
}
