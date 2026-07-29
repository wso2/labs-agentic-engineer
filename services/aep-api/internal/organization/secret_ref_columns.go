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

import "time"

// preferSecretRefString returns the provider-neutral column when set,
// otherwise the legacy sm_api_* value. Used while EXPAND dual-write is
// live (old deployment may still write only sm_api_*).
func preferSecretRefString(newer, older *string) *string {
	if newer != nil {
		return newer
	}
	return older
}

func preferSecretRefTime(newer, older *time.Time) *time.Time {
	if newer != nil {
		return newer
	}
	return older
}

func derefPreferString(newer, older *string) string {
	if p := preferSecretRefString(newer, older); p != nil {
		return *p
	}
	return ""
}

// ResolvedSecretRefName prefers secret_ref_name, falling back to sm_api_*.
func (c *OrgAnthropicCredential) ResolvedSecretRefName() *string {
	return preferSecretRefString(c.SecretRefName, c.SMAPISecretRefName)
}

// ResolvedSecretRefKVPath prefers secret_ref_kv_path, falling back to sm_api_*.
func (c *OrgAnthropicCredential) ResolvedSecretRefKVPath() *string {
	return preferSecretRefString(c.SecretRefKVPath, c.SMAPIKVPath)
}

// ResolvedSecretRefProperty prefers secret_ref_property, falling back to sm_api_*.
func (c *OrgAnthropicCredential) ResolvedSecretRefProperty() *string {
	return preferSecretRefString(c.SecretRefProperty, c.SMAPIProperty)
}

// ResolvedSecretRefName prefers secret_ref_name, falling back to sm_api_*.
func (c *OrgCredential) ResolvedSecretRefName() *string {
	return preferSecretRefString(c.SecretRefName, c.SMAPISecretRefName)
}

// ResolvedSecretRefKVPath prefers secret_ref_kv_path, falling back to sm_api_*.
func (c *OrgCredential) ResolvedSecretRefKVPath() *string {
	return preferSecretRefString(c.SecretRefKVPath, c.SMAPIKVPath)
}

// ResolvedSecretRefProperty prefers secret_ref_property, falling back to sm_api_*.
func (c *OrgCredential) ResolvedSecretRefProperty() *string {
	return preferSecretRefString(c.SecretRefProperty, c.SMAPIProperty)
}

// ResolvedSecretRefWrittenAt prefers secret_ref_written_at, falling back to sm_api_*.
func (c *OrgCredential) ResolvedSecretRefWrittenAt() *time.Time {
	return preferSecretRefTime(c.SecretRefWrittenAt, c.SMAPIWrittenAt)
}

// ResolvedSecretRefName prefers secret_ref_name, falling back to sm_api_*.
func (p *OrganizationIDPProfile) ResolvedSecretRefName() *string {
	return preferSecretRefString(p.SecretRefName, p.SMAPISecretRefName)
}

// ResolvedSecretRefKVPath prefers secret_ref_kv_path, falling back to sm_api_*.
func (p *OrganizationIDPProfile) ResolvedSecretRefKVPath() *string {
	return preferSecretRefString(p.SecretRefKVPath, p.SMAPIKVPath)
}

// ResolvedSecretRefProperty prefers secret_ref_property, falling back to sm_api_*.
func (p *OrganizationIDPProfile) ResolvedSecretRefProperty() *string {
	return preferSecretRefString(p.SecretRefProperty, p.SMAPIProperty)
}

// ResolvedSecretRefWrittenAt prefers secret_ref_written_at, falling back to sm_api_*.
func (p *OrganizationIDPProfile) ResolvedSecretRefWrittenAt() *time.Time {
	return preferSecretRefTime(p.SecretRefWrittenAt, p.SMAPIWrittenAt)
}

// stampSecretRefTriplet returns UpdateColumns keys that dual-write both
// the provider-neutral and legacy sm_api_* column sets.
func stampSecretRefTriplet(secretRefName, vaultKey, prop string) map[string]any {
	return map[string]any{
		"secret_ref_name":        secretRefName,
		"secret_ref_kv_path":     vaultKey,
		"secret_ref_property":    prop,
		"sm_api_secret_ref_name": secretRefName,
		"sm_api_kv_path":         vaultKey,
		"sm_api_property":        prop,
	}
}

// stampSecretRefTripletWithWrittenAt is stampSecretRefTriplet plus the
// written_at pair (org_credentials / organization_idp_profiles).
func stampSecretRefTripletWithWrittenAt(secretRefName, vaultKey, prop string, writtenAt time.Time) map[string]any {
	m := stampSecretRefTriplet(secretRefName, vaultKey, prop)
	m["secret_ref_written_at"] = writtenAt
	m["sm_api_written_at"] = writtenAt
	return m
}

func clearSecretRefTriplet() map[string]any {
	return map[string]any{
		"secret_ref_name":        nil,
		"secret_ref_kv_path":     nil,
		"secret_ref_property":    nil,
		"sm_api_secret_ref_name": nil,
		"sm_api_kv_path":         nil,
		"sm_api_property":        nil,
	}
}

func clearSecretRefTripletWithWrittenAt() map[string]any {
	m := clearSecretRefTriplet()
	m["secret_ref_written_at"] = nil
	m["sm_api_written_at"] = nil
	return m
}
