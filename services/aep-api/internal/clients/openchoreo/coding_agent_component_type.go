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

package openchoreo

// CodingAgentComponentTypeName is the namespaced ComponentType name seeded
// per org. Billing aliases key on this exact string (and job/coding-agent).
const CodingAgentComponentTypeName = "coding-agent"

// CodingAgentComponentTypeRef is what a Component's spec.componentType.name
// carries — {workloadType}/{typeName}. Matches OC's API name format.
const CodingAgentComponentTypeRef = "job/coding-agent"

// CodingAgentComponentType returns the desired namespaced ComponentType body
// for EnsureComponentType. workloadType=job; ExternalSecrets from
// ${dataplane.secretStore}; pins match the retired proxy Job envelope plus
// schema-bounded resources.
//
// Shape is derived from the scheduled-task ClusterComponentType (CronJob)
// by flattening to a batch/v1 Job: schedule / history / concurrency fields
// dropped; ttlSecondsAfterFinished added; restartPolicy Never (side-effectful
// runner — never auto-retry). Pod template labels MUST use
// ${metadata.podSelectors} so the observer query path finds the pod.
func CodingAgentComponentType() map[string]any {
	return map[string]any{
		"apiVersion": "openchoreo.dev/v1alpha1",
		"kind":       "ComponentType",
		"metadata": map[string]any{
			"name": CodingAgentComponentTypeName,
			"annotations": map[string]string{
				"aep.wso2.com/internal": "true",
			},
		},
		"spec": map[string]any{
			"workloadType":  "job",
			"allowedTraits": []any{},
			"parameters": map[string]any{
				"openAPIV3Schema": map[string]any{
					"type": "object",
					"properties": map[string]any{
						// Cost-envelope pins for the cycle Job.
						// backoffLimit maximum 0 — runner has side effects (pushes, PRs).
						"backoffLimit": map[string]any{
							"type": "integer", "default": 0, "maximum": 0,
						},
						// Default 1h; validation dispatches override to 7200 (schema max).
						"activeDeadlineSeconds": map[string]any{
							"type": "integer", "default": 3600, "maximum": 7200,
						},
						"ttlSecondsAfterFinished": map[string]any{
							"type": "integer", "default": 86400,
						},
						"restartPolicy": map[string]any{
							"type": "string", "default": "Never",
							"enum": []any{"Never"},
						},
						// Resource ceilings — schema, not caller, enforces the cap.
						// Requests are enum-bounded too: an unbounded request can
						// schedule-starve the dataplane the same way an unbounded
						// limit can.
						"cpuRequest": map[string]any{
							"type": "string", "default": "500m",
							"enum": []any{"500m", "1"},
						},
						"cpuLimit": map[string]any{
							"type": "string", "default": "1",
							"enum": []any{"500m", "1"},
						},
						"memoryRequest": map[string]any{
							"type": "string", "default": "1Gi",
							"enum": []any{"1Gi", "2Gi"},
						},
						"memoryLimit": map[string]any{
							"type": "string", "default": "2Gi",
							"enum": []any{"1Gi", "2Gi"},
						},
						"imagePullPolicy": map[string]any{
							"type": "string", "default": "IfNotPresent",
							"enum": []any{"Always", "IfNotPresent", "Never"},
						},
					},
				},
			},
			"resources": codingAgentComponentTypeResources(),
		},
	}
}

// codingAgentComponentTypeResources is the CEL-templated resource list:
// one Job plus the ConfigMap / ExternalSecret renders shared with
// scheduled-task (refs-only secrets via ${dataplane.secretStore}).
func codingAgentComponentTypeResources() []any {
	return []any{
		map[string]any{
			"id": "job",
			"template": map[string]any{
				"apiVersion": "batch/v1",
				"kind":       "Job",
				"metadata": map[string]any{
					"name":      "${metadata.name}",
					"namespace": "${metadata.namespace}",
					"labels":    "${metadata.labels}",
				},
				"spec": map[string]any{
					"backoffLimit":            "${parameters.backoffLimit}",
					"activeDeadlineSeconds":   "${parameters.activeDeadlineSeconds}",
					"ttlSecondsAfterFinished": "${parameters.ttlSecondsAfterFinished}",
					"template": map[string]any{
						"metadata": map[string]any{
							// Observer query footgun: must be podSelectors, not labels.
							"labels": "${metadata.podSelectors}",
						},
						"spec": map[string]any{
							"restartPolicy": "${parameters.restartPolicy}",
							"containers": []any{
								map[string]any{
									"name":            "main",
									"image":           "${workload.container.image}",
									"imagePullPolicy": "${parameters.imagePullPolicy}",
									"command":         `${has(workload.container.command) ? workload.container.command : oc_omit()}`,
									"args":            `${has(workload.container.args) ? workload.container.args : oc_omit()}`,
									"resources": map[string]any{
										"requests": map[string]any{
											"cpu":    "${parameters.cpuRequest}",
											"memory": "${parameters.memoryRequest}",
										},
										"limits": map[string]any{
											"cpu":    "${parameters.cpuLimit}",
											"memory": "${parameters.memoryLimit}",
										},
									},
									"env":     "${dependencies.toContainerEnvs()}",
									"envFrom": "${configurations.toContainerEnvFrom()}",
									// Runner workspace contract (emptyDirs)
									// plus any Workload-declared config/secret file mounts.
									"volumeMounts": []any{
										map[string]any{
											"name":      "workspace",
											"mountPath": "/home/aep/aep-workspace",
										},
										map[string]any{
											"name":      "tmp",
											"mountPath": "/tmp",
										},
									},
								},
							},
							"volumes": []any{
								map[string]any{
									"name":     "workspace",
									"emptyDir": map[string]any{},
								},
								map[string]any{
									"name":     "tmp",
									"emptyDir": map[string]any{},
								},
							},
						},
					},
				},
			},
		},
		map[string]any{
			"id":      "env-config",
			"forEach": "${configurations.toConfigEnvsByContainer()}",
			"var":     "envConfig",
			"template": map[string]any{
				"apiVersion": "v1",
				"kind":       "ConfigMap",
				"metadata": map[string]any{
					"name":      "${envConfig.resourceName}",
					"namespace": "${metadata.namespace}",
				},
				"data": `${envConfig.envs.transformMapEntry(index, env, {env.name: env.value})}`,
			},
		},
		map[string]any{
			"id":      "file-config",
			"forEach": "${configurations.toConfigFileList()}",
			"var":     "config",
			"template": map[string]any{
				"apiVersion": "v1",
				"kind":       "ConfigMap",
				"metadata": map[string]any{
					"name":      "${config.resourceName}",
					"namespace": "${metadata.namespace}",
				},
				"data": map[string]any{
					"${config.name}": "${config.value}",
				},
			},
		},
		map[string]any{
			"id":      "secret-env-external",
			"forEach": "${configurations.toSecretEnvsByContainer()}",
			"var":     "secretEnv",
			"template": map[string]any{
				"apiVersion": "external-secrets.io/v1",
				"kind":       "ExternalSecret",
				"metadata": map[string]any{
					"name":      "${secretEnv.resourceName}",
					"namespace": "${metadata.namespace}",
				},
				"spec": map[string]any{
					"refreshInterval": "15s",
					"secretStoreRef": map[string]any{
						"name": "${dataplane.secretStore}",
						"kind": "ClusterSecretStore",
					},
					"target": map[string]any{
						"name":           "${secretEnv.resourceName}",
						"creationPolicy": "Owner",
					},
					"data": `${secretEnv.envs.map(secret, {"secretKey": secret.name, "remoteRef": {"key": secret.remoteRef.key, "property": has(secret.remoteRef.property) ? secret.remoteRef.property : oc_omit()}})}`,
				},
			},
		},
		map[string]any{
			"id":      "secret-file-external",
			"forEach": "${configurations.toSecretFileList()}",
			"var":     "file",
			"template": map[string]any{
				"apiVersion": "external-secrets.io/v1",
				"kind":       "ExternalSecret",
				"metadata": map[string]any{
					"name":      "${file.resourceName}",
					"namespace": "${metadata.namespace}",
				},
				"spec": map[string]any{
					"refreshInterval": "15s",
					"secretStoreRef": map[string]any{
						"name": "${dataplane.secretStore}",
						"kind": "ClusterSecretStore",
					},
					"target": map[string]any{
						"name":           "${file.resourceName}",
						"creationPolicy": "Owner",
					},
					"data": []any{
						map[string]any{
							"secretKey": "${file.name}",
							"remoteRef": map[string]any{
								"key":      "${file.remoteRef.key}",
								"property": `${has(file.remoteRef.property) ? file.remoteRef.property : oc_omit()}`,
							},
						},
					},
				},
			},
		},
	}
}
