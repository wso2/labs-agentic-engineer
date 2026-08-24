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
	"encoding/json"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"gopkg.in/yaml.v3"
)

// PatchCORS adds consoleURL to cors.allowed_origins in Thunder's runtime ConfigMap
// (thunder-config-map, which is mounted into the Thunder pod) and triggers a
// rolling restart so Thunder picks up the new config.
func PatchCORS(ctx context.Context, client *kubernetes.Clientset, consoleURL, namespace, configMap, deployment string) error {
	cmName := configMap
	cm, err := client.CoreV1().ConfigMaps(namespace).Get(ctx, cmName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get ConfigMap %s: %w", cmName, err)
	}

	deployYAML, ok := cm.Data["deployment.yaml"]
	if !ok {
		return fmt.Errorf("ConfigMap %s has no deployment.yaml key", cmName)
	}

	var cfg map[string]interface{}
	if err := yaml.Unmarshal([]byte(deployYAML), &cfg); err != nil {
		return fmt.Errorf("parse deployment.yaml: %w", err)
	}

	cors, _ := cfg["cors"].(map[string]interface{})
	if cors == nil {
		cors = map[string]interface{}{}
		cfg["cors"] = cors
	}

	var origins []string
	rawOrigins, _ := cors["allowed_origins"].([]interface{})
	for _, o := range rawOrigins {
		if s, ok := o.(string); ok {
			if s == consoleURL {
				return nil // already present
			}
			origins = append(origins, s)
		}
	}
	cors["allowed_origins"] = append(origins, consoleURL)

	updatedYAML, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal updated config: %w", err)
	}

	patch, _ := json.Marshal(map[string]interface{}{
		"data": map[string]string{"deployment.yaml": string(updatedYAML)},
	})
	if _, err := client.CoreV1().ConfigMaps(namespace).Patch(
		ctx, cmName, types.MergePatchType, patch, metav1.PatchOptions{},
	); err != nil {
		return fmt.Errorf("patch ConfigMap: %w", err)
	}

	restartPatch, _ := json.Marshal(map[string]interface{}{
		"spec": map[string]interface{}{
			"template": map[string]interface{}{
				"metadata": map[string]interface{}{
					"annotations": map[string]string{
						"kubectl.kubernetes.io/restartedAt": time.Now().UTC().Format(time.RFC3339),
					},
				},
			},
		},
	})
	depName := deployment
	if _, err := client.AppsV1().Deployments(namespace).Patch(
		ctx, depName, types.MergePatchType, restartPatch, metav1.PatchOptions{},
	); err != nil {
		return fmt.Errorf("restart Thunder deployment %s: %w", depName, err)
	}
	return nil
}
