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
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/wso2/aep/aepctl/internal/openbao"
)

var (
	secretImportPath      string
	secretImportValueFile string
)

var secretImportCmd = &cobra.Command{
	Use:   "import",
	Short: "Write a secret into OpenChoreo's built-in OpenBao instance",
	Long: `Writes a single secret value to OpenBao under the AEP secret namespace.

The secret is stored at secret/data/<path> with key "value", matching the
layout that ExternalSecrets expects when syncing AEP platform secrets.

The secret value is read from stdin (masked) by default. For non-interactive
use, supply --value-file pointing to a file containing the secret.

Example:
  aep platform secret import --path aep/anthropic-api-key
  aep platform secret import --path aep/anthropic-api-key --value-file /run/secrets/key`,
	RunE: runSecretImport,
}

func init() {
	secretCmd.AddCommand(secretImportCmd)
	secretImportCmd.Flags().StringVar(&secretImportPath, "path", "", "Secret path under secret/data/ (e.g. aep/anthropic-api-key)")
	secretImportCmd.Flags().StringVar(&secretImportValueFile, "value-file", "", "File containing the secret value (for non-interactive use)")
	_ = secretImportCmd.MarkFlagRequired("path")
}

func runSecretImport(cmd *cobra.Command, args []string) error {
	var value string
	if secretImportValueFile != "" {
		b, err := os.ReadFile(secretImportValueFile)
		if err != nil {
			return fmt.Errorf("read value file: %w", err)
		}
		value = strings.TrimSpace(string(b))
	} else {
		v, err := readMaskedInput("Secret value")
		if err != nil {
			return fmt.Errorf("read secret value: %w", err)
		}
		value = v
	}
	if value == "" {
		return fmt.Errorf("secret value must not be empty")
	}

	ctx := context.Background()

	pfCmd, err := openbao.PortForward(ctx, ocOpenBaoNamespace, ocOpenBaoRelease, kubeconfig)
	if err != nil {
		return fmt.Errorf("port-forward to OpenBao: %w", err)
	}
	defer func() { _ = pfCmd.Process.Kill() }()

	baseURL := "http://localhost:" + openbao.LocalPort
	if err := openbao.WaitForReachable(ctx, baseURL, 30*time.Second); err != nil {
		return fmt.Errorf("OpenBao not reachable via port-forward: %w", err)
	}

	saToken, err := openbao.GetSAToken(ctx, ocOpenBaoNamespace, ocOpenBaoSA, kubeconfig)
	if err != nil {
		return err
	}
	token, err := openbao.KubernetesLogin(ctx, baseURL, ocWriteRole, saToken)
	if err != nil {
		return err
	}

	if _, err := openbao.Must(ctx, "PUT", baseURL, token, "/v1/secret/data/"+secretImportPath, map[string]interface{}{
		"data": map[string]interface{}{"value": value},
	}); err != nil {
		return fmt.Errorf("write secret/data/%s: %w", secretImportPath, err)
	}

	_, _ = fmt.Printf("wrote secret/data/%s\n", secretImportPath)
	return nil
}
