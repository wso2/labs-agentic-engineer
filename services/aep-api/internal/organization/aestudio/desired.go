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

package aestudio

// desired.go — what an org's AE Studio should be: the Resource's
// parameters (a change cuts a ResourceRelease and re-pins) and the binding's
// environment configs (an RRB PUT only), ticket 08 §6. Secret values never
// pass through here: the parameters carry reference names, vault keys and
// properties only.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"

	"github.com/wso2/aep/aep-api/internal/clients/agentsvc"
	"github.com/wso2/aep/aep-api/internal/clients/secretmanagersvc"
	"github.com/wso2/aep/aep-api/internal/config"
	"github.com/wso2/aep/aep-api/internal/organization"
)

// notReadyError is a desired state that cannot be computed yet: a fact of
// the org or the install (not a transient failure), with the state it
// answers. reason is value-free.
type notReadyError struct {
	state  State
	reason string
}

func (e *notReadyError) Error() string { return "ae-studio not ready to ensure: " + e.reason }

func notReady(state State, reason string) error { return &notReadyError{state: state, reason: reason} }

// secretSet is one container's ExternalSecret: its entries and their
// revision. Data is never nil, so it marshals as [] (the RT requires it).
type secretSet struct {
	Rev  string        `json:"rev"`
	Data []secretEntry `json:"data"`
}

// secretEntry is one container env read from the vault: Key and Property
// are copied from the SecretReference's spec.data, never recomputed (06 §2).
type secretEntry struct {
	Env      string `json:"env"`
	Key      string `json:"key"`
	Property string `json:"property"`
}

// params is the Resource's spec.parameters, in the RT's field names.
type params struct {
	Images struct {
		DesignAgent string `json:"designAgent"`
		Collab      string `json:"collab"`
		StudioTools string `json:"studioTools"`
	} `json:"images"`
	Org struct {
		ID     string `json:"id"`
		Handle string `json:"handle"`
	} `json:"org"`
	AgentClientID string `json:"agentClientId"`
	// ModelConnection is the connection's non-secret fields as one JSON
	// string (AE_MODEL_CONNECTION), "" when the org has none (07 §4, R18).
	ModelConnection string `json:"modelConnection"`
	// GitHubOwner is org_credentials.github_login (AE_GITHUB_OWNER).
	GitHubOwner string `json:"githubOwner"`
	Secrets     struct {
		DesignAgent secretSet `json:"designAgent"`
		StudioTools secretSet `json:"studioTools"`
	} `json:"secrets"`
}

// envConfigs is the binding's spec.resourceTypeEnvironmentConfigs, in the
// RT's field names.
type envConfigs struct {
	GatewayHost      string   `json:"gatewayHost"`
	PublicScheme     string   `json:"publicScheme"`
	PublicPortSuffix string   `json:"publicPortSuffix"`
	ListenerName     string   `json:"listenerName"`
	ConsoleOrigins   []string `json:"consoleOrigins"`
	IDP              struct {
		Issuer        string   `json:"issuer"`
		JWKSURL       string   `json:"jwksUrl"`
		TokenURL      string   `json:"tokenUrl"`
		UserAudiences []string `json:"userAudiences"`
	} `json:"idp"`
	AEPAPIBaseURL    string `json:"aepApiBaseUrl"`
	AEOnlyClientID   string `json:"aeOnlyClientId"`
	RuntimeClassName string `json:"runtimeClassName"`
	Cilium           bool   `json:"cilium"`
	Storage          struct {
		SizeLimit        string `json:"sizeLimit"`
		EphemeralRequest string `json:"ephemeralRequest"`
		// BudgetBytes is a string in the RT schema (P-10).
		BudgetBytes string `json:"budgetBytes"`
	} `json:"storage"`
	PullSecret struct {
		RemoteKey string `json:"remoteKey"`
		Property  string `json:"property"`
	} `json:"pullSecret"`
	ExtraEgress json.RawMessage `json:"extraEgress"`
}

// desiredState is everything the converge writes and the drift check
// compares against.
type desiredState struct {
	Params     params
	EnvConfigs envConfigs
}

// fingerprint names this desired state, so a failed converge is retried at
// once when anything it was asked to install changes.
func (d desiredState) fingerprint() string {
	p, _ := json.Marshal(d.Params)
	e, _ := json.Marshal(d.EnvConfigs)
	sum := sha256.Sum256([]byte(TemplateHash() + "\n" + string(p) + "\n" + string(e)))
	return hex.EncodeToString(sum[:])
}

// container names the ExternalSecret a secret entry lands in.
type container int

const (
	studioTools container = iota
	designAgent
)

// secretEnvs maps each (secret, data key) a container reads to its env, in
// the order the container's ExternalSecret lists them.
var secretEnvs = []struct {
	in     container
	secret organization.OrgSecret
	key    string
	env    string
}{
	{studioTools, organization.OrgSecretGitHubPAT, "token", "GITHUB_PAT"},
	{studioTools, organization.OrgSecretGitHubWebhookSecret, "secret", "GITHUB_WEBHOOK_SECRET"},
	{studioTools, organization.OrgSecretPublisherClient, "client_id", "AE_PUBLISHER_CLIENT_ID"},
	{studioTools, organization.OrgSecretPublisherClient, "client_secret", "AE_PUBLISHER_CLIENT_SECRET"},
	{studioTools, organization.OrgSecretStudioClient, "client_id", "AE_STUDIO_CLIENT_ID"},
	{studioTools, organization.OrgSecretStudioClient, "client_secret", "AE_STUDIO_CLIENT_SECRET"},
	{designAgent, organization.OrgSecretDefaultKey, "api-key", "ANTHROPIC_API_KEY"},
}

// requiredForTools are the secrets the tools container cannot start without.
var requiredForTools = []organization.OrgSecret{
	organization.OrgSecretGitHubPAT, organization.OrgSecretGitHubWebhookSecret,
	organization.OrgSecretPublisherClient, organization.OrgSecretStudioClient,
}

// modelConnectionEnv is AE_MODEL_CONNECTION: the turn body's connection
// (its field names pinned by @aep/agent-stream) plus the model.
type modelConnectionEnv struct {
	*agentsvc.TurnConnection
	Model string `json:"model"`
}

// desired computes the org's desired state, reading the secret references
// through oc. A *notReadyError is a state answer, anything else a failure.
func (s *Service) desired(ctx context.Context, oc OC, org string) (desiredState, error) {
	var d desiredState
	refs, err := s.orgSecrets.List(ctx, org)
	if err != nil {
		return d, fmt.Errorf("list org secrets: %w", err)
	}
	set := map[organization.OrgSecret]string{}
	for _, r := range refs {
		set[r.Secret] = r.Name
	}
	if set[organization.OrgSecretGitHubPAT] == "" {
		return d, notReady(StateAbsent, "no GitHub token")
	}
	if err := s.configured(); err != nil {
		return d, err
	}
	for _, sec := range requiredForTools {
		if set[sec] == "" {
			return d, notReady(StateFailed, "org secret "+string(sec)+" is not set")
		}
	}

	o, err := s.orgs.GetByName(ctx, org)
	if err != nil {
		return d, fmt.Errorf("read org: %w", err)
	}
	if o == nil || o.ThunderOrgUUID == nil {
		return d, notReady(StateFailed, "the org has no identity-provider OU recorded")
	}
	profile, err := s.profiles.GetProfileByOrgID(ctx, org)
	if err != nil {
		return d, fmt.Errorf("read idp profile: %w", err)
	}
	if profile == nil || profile.StudioClientID == "" {
		return d, notReady(StateFailed, "the org has no AE Studio client")
	}

	cfg := s.cfg
	d.Params.Images.DesignAgent = cfg.Images.DesignAgent
	d.Params.Images.Collab = cfg.Images.Collab
	d.Params.Images.StudioTools = cfg.Images.StudioTools
	d.Params.Org.ID = o.ThunderOrgUUID.String()
	d.Params.Org.Handle = org
	d.Params.AgentClientID = profile.StudioClientID
	if d.Params.ModelConnection, err = s.modelConnection(ctx, org); err != nil {
		return d, err
	}
	if d.Params.GitHubOwner, err = s.githubOwner(ctx, org); err != nil {
		return d, err
	}
	tools, agent, err := secretSets(ctx, oc, org, set)
	if err != nil {
		return d, err
	}
	d.Params.Secrets.StudioTools, d.Params.Secrets.DesignAgent = tools, agent
	d.EnvConfigs = envConfigsOf(cfg)
	return d, nil
}

// configured reports the install config Ensure needs and lacks as a failed
// state, logging the env names (never values) once per process.
func (s *Service) configured() error {
	missing := s.cfg.Missing()
	var invalid []string
	if !isJSONArray(s.cfg.ExtraEgress) {
		invalid = append(invalid, "AE_STUDIO_EXTRA_EGRESS")
	}
	if len(missing) == 0 && len(invalid) == 0 {
		return nil
	}
	s.notConfiguredOnce.Do(func() {
		slog.Error("ae_studio_not_configured", "missing", missing, "invalid", invalid)
	})
	return notReady(StateFailed, "AE Studio is not configured")
}

// isJSONArray is true for a JSON array; json.Valid alone accepts {} and null.
func isJSONArray(raw json.RawMessage) bool {
	var v []json.RawMessage
	return json.Unmarshal(raw, &v) == nil && v != nil
}

// secretSets builds both containers' ExternalSecret entries from the set
// references. The Default key's entry exists only while its row does (O-5).
func secretSets(ctx context.Context, oc OC, org string, set map[organization.OrgSecret]string) (tools, agent secretSet, err error) {
	byName := map[string]*secretmanagersvc.SecretReference{}
	names := map[container]map[string]bool{studioTools: {}, designAgent: {}}
	tools.Data, agent.Data = []secretEntry{}, []secretEntry{}
	for _, row := range secretEnvs {
		name := set[row.secret]
		if name == "" {
			continue // only the Default key is optional; requiredForTools ran first
		}
		ref, ok := byName[name]
		if !ok {
			ref, err = oc.SecretRefs.GetSecretReference(ctx, org, name)
			if errors.Is(err, secretmanagersvc.ErrNotFound) {
				return tools, agent, notReady(StateFailed, "the reference of org secret "+string(row.secret)+" is missing")
			}
			if err != nil {
				return tools, agent, fmt.Errorf("read the reference of org secret %s: %w", row.secret, err)
			}
			byName[name] = ref
		}
		entry, ok := entryFor(ref, row.key)
		if !ok {
			return tools, agent, notReady(StateFailed, "the reference of org secret "+string(row.secret)+" has no "+row.key)
		}
		entry.Env = row.env
		names[row.in][name] = true
		if row.in == studioTools {
			tools.Data = append(tools.Data, entry)
		} else {
			agent.Data = append(agent.Data, entry)
		}
	}
	tools.Rev, agent.Rev = revOf(names[studioTools]), revOf(names[designAgent])
	return tools, agent, nil
}

// entryFor is the spec.data entry of ref that fills key.
func entryFor(ref *secretmanagersvc.SecretReference, key string) (secretEntry, bool) {
	for _, d := range ref.Data {
		if d.SecretKey == key {
			return secretEntry{Key: d.RemoteKey, Property: d.Property}, true
		}
	}
	return secretEntry{}, false
}

// revOf is the revision of a container's secrets: it changes exactly when
// the set of reference names it reads changes, so a save (a new reference)
// rolls the pod and a no-op save does not. No references: "".
func revOf(names map[string]bool) string {
	if len(names) == 0 {
		return ""
	}
	sorted := make([]string, 0, len(names))
	for n := range names {
		sorted = append(sorted, n)
	}
	sort.Strings(sorted)
	sum := sha256.Sum256([]byte(strings.Join(sorted, ",")))
	return hex.EncodeToString(sum[:])[:16]
}

// modelConnection is AE_MODEL_CONNECTION, "" when the org has no connection.
func (s *Service) modelConnection(ctx context.Context, org string) (string, error) {
	c, ok, err := s.connections.Connection(ctx, org)
	if err != nil {
		return "", fmt.Errorf("read model connection: %w", err)
	}
	if !ok {
		return "", nil
	}
	raw, err := json.Marshal(modelConnectionEnv{TurnConnection: agentsvc.ConnectionFor(c), Model: c.Model})
	if err != nil {
		return "", fmt.Errorf("encode model connection: %w", err)
	}
	return string(raw), nil
}

// githubOwner is the org's GitHub login, "" when it has no GitHub connection.
func (s *Service) githubOwner(ctx context.Context, org string) (string, error) {
	p, err := s.github.Status(ctx, org)
	var nf *organization.NotFoundError
	if errors.As(err, &nf) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read github connection: %w", err)
	}
	return p.GitHubLogin, nil
}

// envConfigsOf maps the install config onto the RT's environmentConfigs.
func envConfigsOf(cfg config.AEStudioConfig) envConfigs {
	var e envConfigs
	e.GatewayHost = cfg.GatewayHost
	e.PublicScheme = cfg.PublicScheme
	e.PublicPortSuffix = cfg.PublicPortSuffix
	e.ListenerName = cfg.ListenerName
	e.ConsoleOrigins = cfg.ConsoleOrigins
	e.IDP.Issuer = cfg.IDP.Issuer
	e.IDP.JWKSURL = cfg.IDP.JWKSURL
	e.IDP.TokenURL = cfg.IDP.TokenURL
	e.IDP.UserAudiences = cfg.IDP.UserAudiences
	e.AEPAPIBaseURL = cfg.AEPAPIBaseURL
	e.AEOnlyClientID = cfg.InternalClientID
	e.RuntimeClassName = cfg.RuntimeClassName
	e.Cilium = cfg.Cilium
	e.Storage.SizeLimit = cfg.Storage.SizeLimit
	e.Storage.EphemeralRequest = cfg.Storage.EphemeralRequest
	e.Storage.BudgetBytes = strconv.FormatInt(cfg.Storage.BudgetBytes, 10)
	e.PullSecret.RemoteKey = cfg.PullSecret.Key
	e.PullSecret.Property = cfg.PullSecret.Property
	e.ExtraEgress = cfg.ExtraEgress
	return e
}
