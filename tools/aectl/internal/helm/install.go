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

// Package helm wraps the helm CLI for operator installation.
package helm

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"

	"github.com/wso2/aep/aectl/internal/addons"
)

// InstallOperator runs `helm upgrade --install` for the given OperatorSpec.
// kubeconfig is forwarded via --kubeconfig; an empty string uses the default.
// On failure, combined stdout+stderr from helm is included in the returned error.
func InstallOperator(ctx context.Context, kubeconfig string, op addons.OperatorSpec) error {
	args := []string{
		"upgrade", "--install", op.ReleaseName, op.Chart,
		"-n", op.Namespace, "--create-namespace",
		"--wait", "--timeout", "5m",
	}
	if op.Version != "" {
		args = append(args, "--version", op.Version)
	}
	for _, s := range op.Sets {
		args = append(args, "--set", s)
	}
	if kubeconfig != "" {
		args = append(args, "--kubeconfig", kubeconfig)
	}

	var out bytes.Buffer
	cmd := exec.CommandContext(ctx, "helm", args...)
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w\n%s", err, out.String())
	}
	return nil
}
