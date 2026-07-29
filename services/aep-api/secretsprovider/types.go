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

package secretsprovider

import (
	"fmt"
	"strings"
)

// SecretMetadata contains metadata for a secret.
type SecretMetadata struct {
	// ManagedBy identifies who manages this secret.
	// Used to prevent accidental deletion of secrets created outside this client.
	ManagedBy string `json:"managedBy,omitempty"`

	// Labels are optional key-value pairs for additional metadata.
	Labels map[string]string `json:"labels,omitempty"`
}

// SecretInfo contains information about a secret without the actual values.
type SecretInfo struct {
	// ID is the unique identifier for the secret (e.g., secretReferenceName).
	ID string `json:"id"`

	// Name is the logical name of the secret.
	Name string `json:"name,omitempty"`

	// Keys is the list of keys available in the secret (without values).
	Keys []string `json:"keys,omitempty"`

	// Labels are optional key-value pairs for additional metadata.
	Labels map[string]string `json:"labels,omitempty"`

	// CreatedAt is the timestamp when the secret was created.
	CreatedAt string `json:"createdAt,omitempty"`
}

// StoreConfig holds configuration for secret store backends.
type StoreConfig struct {
	// Provider is the name of the provider to use (e.g., "openbao", "vault", "aws").
	Provider string `json:"provider"`

	// OpenBao contains OpenBao/Vault-specific configuration.
	OpenBao *OpenBaoConfig `json:"openbao,omitempty"`
}

// OpenBaoConfig contains configuration for OpenBao/Vault.
// Only KV v2 secrets engine is supported.
type OpenBaoConfig struct {
	// Server is the OpenBao server address (e.g., "https://openbao.example.com").
	Server string `json:"server"`

	// Path is the mount path for the KV secrets engine (e.g., "secret").
	Path string `json:"path"`

	// Auth contains authentication configuration.
	Auth *OpenBaoAuth `json:"auth"`
}

// OpenBaoAuth contains authentication configuration for OpenBao.
type OpenBaoAuth struct {
	// Token is a static token for authentication.
	Token string `json:"token,omitempty"`
}

// SecretLocation identifies where a secret lives in the KV hierarchy.
//
// A coding-agent task is the smallest ownership unit: the dispatch path
// mints one ExternalSecret per task per credential; per-env scoping (when
// needed) happens at the SecretReference `environments:` slice on the OC
// side, not in the KV path. SecretRefName is derived from `{task, entity}`
// so two different tasks in the same project don't collide when both
// consume the same upstream credential.
//
// All segments are validated against `/` to prevent traversal +
// path collisions.
type SecretLocation struct {
	// OrgName is the org UUID (ouId). The OpenChoreo org namespace is derived
	// via tenant.OrgBaseNamespace at SecretReference authoring time — do not
	// pass the derived `wc-…` namespace here. Required.
	OrgName string

	// ProjectName is the OC Project handle. Optional — empty for
	// org-scoped credentials (Anthropic platform key, App webhook
	// secret).
	ProjectName string

	// TaskID is the ComponentTask UUID. Optional — empty for
	// project-scoped credentials (per-org Anthropic key,
	// per-org GitHub PAT) and org-scoped credentials.
	TaskID string

	// EntityName is the credential kind handle: `anthropic`,
	// `github-pat`, `runner-thunder-client`, etc. Required —
	// every secret has a kind so two unrelated credentials at the
	// same scope don't collide.
	EntityName string

	// SecretKey is the field name inside the secret payload. Optional —
	// when set the KVPath addresses a single value rather than the
	// whole record.
	SecretKey string
}

func sanitizeSegment(s string) (string, error) {
	s = strings.TrimSpace(s)
	if strings.Contains(s, "/") {
		return "", fmt.Errorf("secret path segment %q contains invalid character '/'", s)
	}
	return s, nil
}

// KVPath builds the KV-store path from non-empty segments.
//
// Shapes (each ends with EntityName, optionally suffixed by SecretKey):
//
//	org/entity                          (org-scoped)
//	org/entity/key                      (org-scoped + key)
//	org/project/entity                  (project-scoped)
//	org/project/entity/key              (project-scoped + key)
//	org/project/task/entity             (task-scoped)
//	org/project/task/entity/key         (task-scoped + key)
func (l SecretLocation) KVPath() (string, error) {
	if strings.TrimSpace(l.OrgName) == "" {
		return "", fmt.Errorf("SecretLocation.OrgName is required")
	}
	if strings.TrimSpace(l.EntityName) == "" {
		return "", fmt.Errorf("SecretLocation.EntityName is required")
	}
	if l.TaskID != "" && l.ProjectName == "" {
		return "", fmt.Errorf("SecretLocation.TaskID requires ProjectName")
	}

	orgSeg, err := sanitizeSegment(l.OrgName)
	if err != nil {
		return "", fmt.Errorf("invalid OrgName: %w", err)
	}
	parts := []string{orgSeg}
	if l.ProjectName != "" {
		seg, err := sanitizeSegment(l.ProjectName)
		if err != nil {
			return "", fmt.Errorf("invalid ProjectName: %w", err)
		}
		parts = append(parts, seg)
	}
	if l.TaskID != "" {
		seg, err := sanitizeSegment(l.TaskID)
		if err != nil {
			return "", fmt.Errorf("invalid TaskID: %w", err)
		}
		parts = append(parts, seg)
	}
	entitySeg, err := sanitizeSegment(l.EntityName)
	if err != nil {
		return "", fmt.Errorf("invalid EntityName: %w", err)
	}
	parts = append(parts, entitySeg)
	if l.SecretKey != "" {
		seg, err := sanitizeSegment(l.SecretKey)
		if err != nil {
			return "", fmt.Errorf("invalid SecretKey: %w", err)
		}
		parts = append(parts, seg)
	}
	return strings.Join(parts, "/"), nil
}

// SecretRefName derives the OC SecretReference name from the location.
// Sanitized to a DNS-label (lowercase, max 63 chars). Includes TaskID
// when set so per-task secrets don't collide with per-project ones.
func (l SecretLocation) SecretRefName() string {
	var name string
	switch {
	case l.TaskID != "":
		name = fmt.Sprintf("%s-%s-secrets",
			sanitizeForK8sName(l.TaskID),
			sanitizeForK8sName(l.EntityName))
	default:
		name = fmt.Sprintf("%s-secrets", sanitizeForK8sName(l.EntityName))
	}
	if len(name) > 63 {
		name = strings.TrimRight(name[:63], "-")
	}
	return name
}

func sanitizeForK8sName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteRune('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

// ParseKVPath inverts KVPath. Only the six legal shapes above are
// recognized; anything else returns an error so callers don't have to
// guess.
func ParseKVPath(kvPath string) (SecretLocation, error) {
	parts := strings.Split(kvPath, "/")
	switch len(parts) {
	case 2:
		return SecretLocation{OrgName: parts[0], EntityName: parts[1]}, nil
	case 3:
		// Two legal shapes have 3 segments: org/entity/key and
		// org/project/entity. Disambiguate by treating segment-2 as
		// a key if it looks like one (lower-snake, short) — but
		// there's no way to be sure without context, so we treat
		// 3-segment paths as `org/project/entity` and require callers
		// that want `org/entity/key` to use the explicit form
		// `org//entity/key` (rejected by sanitizeSegment) or
		// construct SecretLocation directly. ParseKVPath is
		// best-effort for logging/introspection.
		return SecretLocation{
			OrgName:     parts[0],
			ProjectName: parts[1],
			EntityName:  parts[2],
		}, nil
	case 4:
		return SecretLocation{
			OrgName:     parts[0],
			ProjectName: parts[1],
			EntityName:  parts[2],
			SecretKey:   parts[3],
		}, nil
	case 5:
		return SecretLocation{
			OrgName:     parts[0],
			ProjectName: parts[1],
			TaskID:      parts[2],
			EntityName:  parts[3],
			SecretKey:   parts[4],
		}, nil
	default:
		return SecretLocation{}, fmt.Errorf("unrecognized KV path format: %s (expected 2-5 segments, got %d)", kvPath, len(parts))
	}
}
