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

package dependencies

// Consumer-side ports. The dependencies package has a tightly scoped
// arch-allowlist row (internal/arch/arch_test.go) for its external-resource
// slice — it imports NO other feature package for this concern. Every
// collaborator is expressed as a narrow interface here and wired concretely in
// the composition root.

import (
	"context"

	"github.com/wso2/aep/aep-api/internal/spec"
)

// SecretWriter is the slice of the secret-ref writer the provisioner needs.
// Satisfied by *organization.SecretRefWriter.
type SecretWriter interface {
	Enabled() bool
	// WriteExternalResourceSecret uploads the secret fields for a
	// (project, entity) and returns the Vault KV path the ExternalSecret
	// reads (the secretStorePath).
	WriteExternalResourceSecret(ctx context.Context, ocOrgID, projectName, entityName string, data map[string]string) (vaultKey, secretRefName string, err error)
}

// NOTE (dependency-management migration): the task-coupled ports the
// value/resource services used — TaskStore (read the component_tasks repo),
// TaskCompleter (drive ComponentTask.Status through the projector) and
// RedispatchFunc — were removed here at the merge along with their only
// consumers (external_values.go / resources_service.go, git-rm'd). Phase 6
// rebuilt the value/param surface in internal/feature/provisioning on our
// GitHub-native `provision` gate funnel; the completion port it uses there is
// "close the provision issue" (consumer release via gate-close webhook +
// eventcore sweep), not a component_tasks
// projector. The org-level external-resource catalog (list/delete + the
// consumer scan for the in-use delete guard) also lives there now: the
// provisioning ExternalRTCatalog port, backed by resources.ExternalResourceCatalog
// over the OpenChoreo client (org-namespaced ResourceTypes), plus the design-scan
// consumers. The old external_resources DB table + its repository were removed.

// DesignReader is the slice of the design store the resource provisioner
// reads: the project's authored design components, whose platform-resource
// entries carry the ClusterResourceType to provision and whose external
// entries carry the config[] schema (with Secret flags) ResolveRunnerSecrets
// classifies by — the same schema the build path reads, so neither path
// consults the org catalog for secret classification. It deliberately returns
// ONLY models-typed data — NOT artifacts.DesignFile — so this package keeps
// its empty arch-allowlist row (no dependencies/resources → artifacts feature
// edge). The composition root adapts artifacts.ArtifactStore.ReadDesign with
// a one-line wrapper returning design.Components ((nil, nil) design ⇒ nil
// components — "no design yet").
type DesignReader interface {
	ReadDesignComponents(ctx context.Context, orgID, projectID string) ([]spec.DesignComponent, error)
}
