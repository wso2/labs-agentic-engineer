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

// codingAgentDeadlineCeilingSeconds is the schema's activeDeadlineSeconds
// maximum (3h). It bounds BOTH cycle kinds: a validation cycle passes 2h and a
// coding cycle 3h, and the schema — never the caller — is what rejects anything
// past it.
const codingAgentDeadlineCeilingSeconds = 10800

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
						// Default 1h for a dispatch that names no deadline; every
						// dispatch does name one — a coding cycle 10800 (3h: it now
						// ends with a browser verification wave, which an hour reaps
						// mid-run) and a validation cycle 7200. The maximum is the
						// larger of the two, so it is what actually bounds the Job.
						"activeDeadlineSeconds": map[string]any{
							"type": "integer", "default": 3600, "maximum": codingAgentDeadlineCeilingSeconds,
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
						//
						// The REQUEST stays small and the LIMIT is what moved: a
						// request is a reservation held for the pod's whole life,
						// while a limit is only a ceiling it may burst to. A runner
						// spends most of its wall clock waiting on the model — its
						// own cgroup shows ~34% of one core averaged over a run —
						// and then wants several cores for a few bursts of `npm
						// install` and `bal build`. Reserving 500m and allowing 3
						// buys those bursts without holding cores idle in between.
						//
						// Under contention the split is also what protects the rest
						// of the dataplane: cgroup CPU weight is proportional to
						// REQUESTS, so a bursting runner is squeezed back toward its
						// 500m share as soon as anything else becomes runnable,
						// rather than holding 3 cores against it.
						"cpuRequest": map[string]any{
							"type": "string", "default": "500m",
							"enum": []any{"500m", "1"},
						},
						"cpuLimit": map[string]any{
							"type": "string", "default": "3",
							"enum": []any{"500m", "1", "2", "3"},
						},
						// Memory does NOT follow the CPU split above, because
						// memory is not compressible: overrunning a CPU limit costs
						// throttling, overrunning a memory limit costs an OOM kill,
						// and sitting above the REQUEST makes a Burstable pod the
						// first thing evicted when the node comes under pressure. A
						// runner loses its whole cycle either way.
						//
						// Mock verification put a Chromium and a Vite dev server
						// inside this pod. Measured in the runner image against a
						// real web-application fixture, that phase alone peaks at
						// 1.22 GiB (cgroup `memory.peak`, npm install + build +
						// live browser concurrently) — above the 1Gi that used to
						// be reserved here. So the request now covers the floor the
						// work actually stands on, and the limit leaves room above
						// it for the agent process and for a heavier page than the
						// fixture's four-row table.
						"memoryRequest": map[string]any{
							"type": "string", "default": "2Gi",
							"enum": []any{"1Gi", "2Gi", "3Gi"},
						},
						"memoryLimit": map[string]any{
							"type": "string", "default": "3Gi",
							"enum": []any{"1Gi", "2Gi", "3Gi", "4Gi"},
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
