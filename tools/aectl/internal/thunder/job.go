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
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// BuildAuthJob constructs a Job that registers AEP OAuth clients in Thunder
// using Bearer token auth (scope=system). Runs inside the cluster so it can
// reach the in-cluster Thunder service URL.
//
// Uses alpine/curl — no Thunder image, no PVC, no setup.sh.
// secretName must contain all AEP client secrets plus THUNDER_ADMIN_CLIENT_ID
// and THUNDER_ADMIN_CLIENT_SECRET. scriptCMName must have a "setup.sh" key.
func BuildAuthJob(name, namespace, secretName, scriptCMName string) *batchv1.Job {
	backoffLimit := int32(2)
	ttl := int32(600)
	scriptMode := int32(0555)

	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    map[string]string{"app.kubernetes.io/managed-by": "aep"},
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoffLimit,
			TTLSecondsAfterFinished: &ttl,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app.kubernetes.io/component": "thunder-setup"},
				},
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyOnFailure,
					Containers: []corev1.Container{
						{
							Name:    "setup",
							Image:   "alpine/curl:latest",
							Command: []string{"sh", "/scripts/setup.sh"},
							EnvFrom: []corev1.EnvFromSource{
								{SecretRef: &corev1.SecretEnvSource{
									LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
								}},
							},
							VolumeMounts: []corev1.VolumeMount{
								{Name: "scripts", MountPath: "/scripts", ReadOnly: true},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "scripts",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{Name: scriptCMName},
									DefaultMode:          &scriptMode,
								},
							},
						},
					},
				},
			},
		},
	}
}
