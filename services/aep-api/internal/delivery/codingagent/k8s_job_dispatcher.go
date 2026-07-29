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

package codingagent

import (
	"context"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

// errK8sJobSecretDeliveryRemoved is returned by K8sJobDispatcher.Dispatch.
// Runner secrets are refs-only via the cluster-gateway-proxy path (per-run
// ExternalSecrets). This dispatcher must not write Secret or ExternalSecret;
// phase 08 owns replacing the direct k8s-job mechanism.
const errK8sJobSecretDeliveryRemoved = "k8s-job: plaintext secret delivery removed; configure cluster-gateway-proxy + secret refs"

// K8sJobInput collects everything needed to dispatch one direct K8s Job.
// Retained for phase 08 (replacement of this dispatcher); Dispatch currently
// rejects all calls because this path cannot deliver runner secrets.
type K8sJobInput struct {
	RunName       string
	OrgID         string
	OrgUUID       string // used to derive the data-plane namespace
	ProjectID     string
	Component     string
	ExecutionID   string // stamped as AEP_TASK_ID (naming debt: carries the execution id)
	RepoURL       string
	Prompt        string
	IdentityName  string
	IdentityEmail string
	IdentityLogin string
	Bearer        string
	SkillsRepoURL string
}

// K8sJobDispatcher is the direct in-cluster Job dispatch path. It remains
// constructible and wired as a fallback mechanism, but Dispatch refuses to run:
// plaintext Anthropic Secret writes are gone, and ExternalSecret materialization
// belongs to the proxy Dispatcher — not a second secrets path here (phase 08).
type K8sJobDispatcher struct {
	client      client.Client
	platformURL string
	runnerImage string
}

// NewK8sJobDispatcher constructs the dispatcher. All parameters are required
// so phase 08 can resume Job creation without reshaping the composition root.
func NewK8sJobDispatcher(cl client.Client, platformURL, runnerImage string) *K8sJobDispatcher {
	return &K8sJobDispatcher{client: cl, platformURL: platformURL, runnerImage: runnerImage}
}

// Dispatch always fails: this path cannot deliver runner secrets (refs-only via
// cluster-gateway-proxy). It does not create Namespace, ServiceAccount, Job,
// Secret, or ExternalSecret.
func (d *K8sJobDispatcher) Dispatch(_ context.Context, _ K8sJobInput) (string, error) {
	// Touch ctor fields so they stay wired for phase 08 without inventing a
	// second secrets path here.
	_ = d.client
	_ = d.platformURL
	_ = d.runnerImage
	return "", fmt.Errorf("%s", errK8sJobSecretDeliveryRemoved)
}
