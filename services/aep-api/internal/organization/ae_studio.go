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

package organization

// ae_studio.go — the ports an org's AE Studio (ADR-0045) is reached
// through. The aestudio sub-package implements both; the root and its
// slices only ever hold the ports, so no slice imports aestudio.

import "context"

// AEStudioState is where an org's AE Studio stands.
type AEStudioState string

const (
	// AEStudioAbsent: the org has no GitHub token, so there is nothing to
	// install yet.
	AEStudioAbsent AEStudioState = "absent"
	// AEStudioProvisioning: a converge is running, or the pod is rolling.
	AEStudioProvisioning AEStudioState = "provisioning"
	// AEStudioReady: the installed AE Studio matches what the org wants and
	// its binding is Ready.
	AEStudioReady AEStudioState = "ready"
	// AEStudioFailed: AE Studio cannot be installed as configured, or the last
	// converge failed moments ago.
	AEStudioFailed AEStudioState = "failed"
)

// AEStudioFailReason is why a failed AE Studio failed.
type AEStudioFailReason string

const (
	// AEStudioFailTimeout: the binding has not become Ready within the
	// bound. OpenChoreo does not say why (a pod that cannot be scheduled
	// reads the same as any other), and it may still come up by itself.
	AEStudioFailTimeout AEStudioFailReason = "timeout"
	// AEStudioFailError: every other failure (AE Studio not configured, no
	// write target, a failed converge, a release OpenChoreo cannot render
	// or own).
	AEStudioFailError AEStudioFailReason = "error"
)

// AEStudioURLs are the public URLs of a ready AE Studio's three services.
type AEStudioURLs struct {
	DesignAgent string
	Collab      string
	Tools       string
}

// AEStudioStatus is an org's AE Studio state. URLs is set only when ready,
// Reason only when failed.
// OUID is the OU id the pod pins (its parameters.org.id); it is never on the
// wire, and is what a caller of the pod's internal API sends as
// X-Impersonate-Org.
type AEStudioStatus struct {
	State  AEStudioState
	URLs   *AEStudioURLs
	Reason AEStudioFailReason
	OUID   string
}

// StudioConverger starts a converge of the org's AE Studio and returns at
// once; the converge runs detached from the caller's request. org is the
// org's OpenChoreo namespace (its handle).
type StudioConverger interface {
	Trigger(ctx context.Context, org string)
}

// AEStudioStatusReader answers GET /ae-studio: the org's state, starting a
// converge (and answering provisioning) when what is installed has drifted
// from what the org wants. It never waits for the converge.
type AEStudioStatusReader interface {
	Status(ctx context.Context, org string) (AEStudioStatus, error)
}
