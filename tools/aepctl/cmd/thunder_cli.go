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
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"

	"github.com/spf13/viper"
	k8s "github.com/wso2/aep/aepctl/internal/kubernetes"
	"github.com/wso2/aep/aepctl/internal/thunder"
)

// registerThunderFlags adds Thunder connection flags to cmd and binds each one
// to its corresponding Viper key. Precedence: flag > AEP_* env var > cluster ConfigMap > default.
func registerThunderFlags(cmd *cobra.Command) {
	f := cmd.Flags()
	f.String("thunder-namespace", "", "Kubernetes namespace where Thunder is installed")
	f.String("thunder-url", "", "In-cluster URL of the Thunder service")
	f.String("thunder-config-map", "", "Name of Thunder's runtime ConfigMap")
	f.String("thunder-deployment", "", "Name of Thunder's Deployment")
	f.String("thunder-admin-client-id", "", "Thunder admin OAuth client ID")
	f.String("thunder-public-url", "", "Public URL of Thunder — must match the JWT issuer configured in Thunder")

	_ = viper.BindPFlag("thunder.namespace", f.Lookup("thunder-namespace"))
	_ = viper.BindPFlag("thunder.url", f.Lookup("thunder-url"))
	_ = viper.BindPFlag("thunder.config_map", f.Lookup("thunder-config-map"))
	_ = viper.BindPFlag("thunder.deployment", f.Lookup("thunder-deployment"))
	_ = viper.BindPFlag("thunder.admin_client_id", f.Lookup("thunder-admin-client-id"))
	_ = viper.BindPFlag("thunder.public_url", f.Lookup("thunder-public-url"))
}

// doThunderSetup registers all AEP OAuth clients in the running Thunder instance
// by submitting a K8s Job that runs inside the cluster (in-cluster DNS required).
//
// The Job uses the OC system client (scope=system) to authenticate against Thunder
// and then registers all AEP clients via the admin API — no PVC, no setup.sh,
// no security bypass.
//
// thunderAdminClientSecret is prompted masked if not provided (empty string).
func doThunderSetup(
	ctx context.Context,
	client *kubernetes.Clientset,
	aepNamespace, thunderNamespace, thunderURL, thunderConfigMap, thunderDeployment, thunderAdminClientID, consoleURL string,
) error {

	step := func(msg string) { _, _ = fmt.Fprintf(os.Stdout, "  %s\n", msg) }

	thunderAdminClientSecret := viper.GetString("thunder.admin_client_secret")

	jobName := "aep-thunder-setup"
	secretName := "aep-thunder-setup-creds"
	cmName := "aep-thunder-setup-script"

	// 1. Read AEP client secrets from the ESO-synced Secret.
	step("Reading AEP client secrets")
	src, err := client.CoreV1().Secrets(aepNamespace).Get(ctx, "aep-thunder-secrets", metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("read aep-thunder-secrets: %w (run `aep init` first)", err)
	}
	secretKeys := map[string]string{
		"OC_WORKLOAD_PUBLISHER_SECRET": "OC_WORKLOAD_PUBLISHER_SECRET",
		"OC_OBSERVER_READER_SECRET":    "OC_OBSERVER_READER_SECRET",
		"AEP_API_CLIENT_SECRET":        "AEP_API_CLIENT_SECRET",
		"BFF_TO_GIT_SERVICE_SECRET":    "BFF_TO_GIT_SERVICE_SECRET",
		"BFF_TO_REMOTE_WORKER_SECRET":  "BFF_TO_REMOTE_WORKER_SECRET",
		"LOCAL_DEV_SEEDER_SECRET":      "LOCAL_DEV_SEEDER_SECRET",
		"THUNDER_SYSTEM_CLIENT_SECRET": "AEP_SYSTEM_CLIENT_SECRET",
		"OC_RCA_AGENT_SECRET":          "OC_RCA_AGENT_SECRET",
	}
	secretData := make(map[string][]byte, len(secretKeys)+2)
	for srcKey, destKey := range secretKeys {
		v := src.Data[srcKey]
		if len(v) == 0 {
			return fmt.Errorf("key %q missing in aep-thunder-secrets — run `aep init` first", srcKey)
		}
		secretData[destKey] = v
	}
	secretData["THUNDER_ADMIN_CLIENT_ID"] = []byte(thunderAdminClientID)
	secretData["THUNDER_ADMIN_CLIENT_SECRET"] = []byte(thunderAdminClientSecret)

	// 2. Create (or replace) the short-lived credentials Secret.
	_ = client.CoreV1().Secrets(aepNamespace).Delete(ctx, secretName, metav1.DeleteOptions{})
	_, err = client.CoreV1().Secrets(aepNamespace).Create(ctx, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: aepNamespace,
			Labels:    map[string]string{"app.kubernetes.io/managed-by": "aep"},
		},
		Data: secretData,
	}, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("create setup credentials secret: %w", err)
	}
	defer func() {
		dctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = client.CoreV1().Secrets(aepNamespace).Delete(dctx, secretName, metav1.DeleteOptions{})
	}()

	// 3. Create (or replace) the ConfigMap containing the bootstrap script.
	script := thunder.AuthenticatedScript(thunderURL, consoleURL)
	cmData, _ := json.Marshal(map[string]interface{}{
		"data": map[string]string{"setup.sh": script},
	})
	_, cmErr := client.CoreV1().ConfigMaps(aepNamespace).Get(ctx, cmName, metav1.GetOptions{})
	if cmErr == nil {
		if _, err := client.CoreV1().ConfigMaps(aepNamespace).Patch(
			ctx, cmName, types.MergePatchType, cmData, metav1.PatchOptions{},
		); err != nil {
			return fmt.Errorf("patch setup script ConfigMap: %w", err)
		}
	} else {
		if _, err := client.CoreV1().ConfigMaps(aepNamespace).Create(ctx, &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      cmName,
				Namespace: aepNamespace,
				Labels:    map[string]string{"app.kubernetes.io/managed-by": "aep"},
			},
			Data: map[string]string{"setup.sh": script},
		}, metav1.CreateOptions{}); err != nil {
			return fmt.Errorf("create setup script ConfigMap: %w", err)
		}
	}
	defer func() {
		dctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = client.CoreV1().ConfigMaps(aepNamespace).Delete(dctx, cmName, metav1.DeleteOptions{})
	}()

	// 4. Delete any stale Job from a previous run.
	fg := metav1.DeletePropagationForeground
	if err := client.BatchV1().Jobs(aepNamespace).Delete(ctx, jobName, metav1.DeleteOptions{
		PropagationPolicy: &fg,
	}); err == nil {
		step("Waiting for previous job to be deleted")
		for range 30 {
			if _, err := client.BatchV1().Jobs(aepNamespace).Get(ctx, jobName, metav1.GetOptions{}); err != nil {
				break
			}
			time.Sleep(2 * time.Second)
		}
	}

	// 5. Run the setup Job and wait for completion.
	step("Running Thunder setup job")
	job := thunder.BuildAuthJob(jobName, aepNamespace, secretName, cmName)
	if err := k8s.RunJob(ctx, client, job, os.Stdout); err != nil {
		return fmt.Errorf("setting up Thunder failed: %w", err)
	}

	// 6. Patch Thunder's CORS configuration and restart (CLI has direct k8s access).
	step("Patching Thunder CORS configuration")
	if err := thunder.PatchCORS(ctx, client, consoleURL, thunderNamespace, thunderConfigMap, thunderDeployment); err != nil {
		step(fmt.Sprintf("Warning: CORS patch failed (%v). Add %s manually if needed.", err, consoleURL))
	} else {
		step("Thunder CORS configured")
	}

	step("Thunder setup complete")
	return nil
}
