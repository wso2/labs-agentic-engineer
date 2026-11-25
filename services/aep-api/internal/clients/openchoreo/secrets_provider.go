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

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo/gen"
	"github.com/wso2/aep/aep-api/internal/clients/secretmanagersvc"
)

// SecretTargetPlane is the plane OpenChoreo stores secret values on. The
// plane's own secret store (its secretStoreRef) decides the backend, so
// aep-api never names one.
type SecretTargetPlane struct {
	Kind string
	Name string
}

// secretsProvider stores secrets through OpenChoreo's Secret API. OpenChoreo
// writes the value to the target plane's secret store and creates the
// SecretReference itself, so the provider manages SecretReferences.
type secretsProvider struct {
	client *secretsClient
}

var (
	_ secretmanagersvc.Provider               = (*secretsProvider)(nil)
	_ secretmanagersvc.SecretReferenceManager = (*secretsProvider)(nil)
	_ secretmanagersvc.SecretsClient          = (*secretsClient)(nil)
	_ secretmanagersvc.StoredKeyResolver      = (*secretsClient)(nil)
)

// NewSecretsProvider builds the provider over OpenChoreo's Secret API. Panics
// on construction failure (same pattern as NewSecretReferenceClient).
func NewSecretsProvider(cfg Config, plane SecretTargetPlane) secretmanagersvc.Provider {
	oc, err := newGenClient(cfg)
	if err != nil {
		panic(fmt.Errorf("init openchoreo secrets provider: %w", err))
	}
	return &secretsProvider{client: &secretsClient{oc: oc, plane: plane}}
}

func (p *secretsProvider) NewClient(*secretmanagersvc.StoreConfig) (secretmanagersvc.SecretsClient, error) {
	return p.client, nil
}

func (p *secretsProvider) ValidateConfig(*secretmanagersvc.StoreConfig) error { return nil }

func (p *secretsProvider) Capabilities() secretmanagersvc.StoreCapabilities {
	return secretmanagersvc.StoreCapabilityReadWrite
}

func (p *secretsProvider) ManagesSecretReferences() bool { return true }

type secretsClient struct {
	oc    *gen.ClientWithResponses
	plane SecretTargetPlane
}

// PushSecret stores value (one JSON object of key → value) as the secret
// named location.SecretRefName() in the location's control-plane namespace,
// and returns that name: it is also the name of the SecretReference
// OpenChoreo creates.
//
// A location with RefName set asks for a new secret: a name already in use
// is ErrConflict, and the existing secret is left alone. Without RefName the
// name is fixed by the location and saving again replaces the stored data.
func (c *secretsClient) PushSecret(ctx context.Context, location secretmanagersvc.SecretLocation, value []byte, _ *secretmanagersvc.SecretMetadata) (string, error) {
	ns, err := location.CPNamespace()
	if err != nil {
		return "", err
	}
	var data map[string]string
	if err := json.Unmarshal(value, &data); err != nil {
		return "", fmt.Errorf("openchoreo secrets: unmarshal secret data: %w", err)
	}
	name := location.SecretRefName()
	status, err := c.create(ctx, ns, name, data)
	switch {
	case err == nil:
		return name, nil
	case location.RefName == "" && errors.Is(err, secretmanagersvc.ErrConflict):
		if err := c.update(ctx, ns, name, data); err != nil {
			return "", err
		}
		return name, nil
	case location.RefName != "" && mayBeHalfDone(status):
		return "", c.discardHalfDone(ctx, ns, name, data, err)
	}
	return "", err
}

// mayBeHalfDone reports whether a failed create may have left work behind.
// OpenChoreo checks the request and the name before it writes anything, so
// a 4xx is a clean refusal. A 5xx, or no response at all, can come after it
// already copied the value to the plane but before it created the
// SecretReference.
func mayBeHalfDone(status int) bool {
	return status == 0 || status >= http.StatusInternalServerError
}

// cleanupTimeout bounds discardHalfDone's two calls together.
const cleanupTimeout = 30 * time.Second

// discardHalfDone removes what a failed create of a new secret may have left
// behind. OpenChoreo's DeleteSecret only finds a secret through its
// SecretReference, so a copy written before the reference existed is
// invisible to it. Creating the same name again completes the secret
// (OpenChoreo's writes are idempotent), and then it can be deleted. The
// name is freshly minted, so a 409 here also means the secret is this
// write's own. cause is always returned: the caller's write failed.
//
// The cleanup runs on its own context: a write that failed because the
// caller's context was cancelled or timed out is exactly the one most likely
// to have left a copy behind. It keeps ctx's values (the auth claims) and is
// bounded by cleanupTimeout.
func (c *secretsClient) discardHalfDone(ctx context.Context, ns, name string, data map[string]string, cause error) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
	defer cancel()
	if _, err := c.create(ctx, ns, name, data); err != nil && !errors.Is(err, secretmanagersvc.ErrConflict) {
		return fmt.Errorf("%w (cleaning up %s/%s also failed: %v)", cause, ns, name, err)
	}
	if err := c.delete(ctx, ns, name); err != nil {
		return fmt.Errorf("%w (cleaning up %s/%s also failed: %v)", cause, ns, name, err)
	}
	return cause
}

// create returns the HTTP status alongside the error; 0 means no response.
func (c *secretsClient) create(ctx context.Context, ns, name string, data map[string]string) (int, error) {
	resp, err := c.oc.CreateSecretWithResponse(ctx, ns, gen.CreateSecretJSONRequestBody{
		SecretName: name,
		SecretType: gen.SecretTypeOpaque,
		Data:       data,
		TargetPlane: gen.TargetPlaneRef{
			Kind: gen.TargetPlaneRefKind(c.plane.Kind),
			Name: c.plane.Name,
		},
	})
	if err != nil {
		return 0, fmt.Errorf("openchoreo secrets: create %s/%s: %w", ns, name, err)
	}
	if resp.StatusCode() != http.StatusCreated {
		return resp.StatusCode(), fmt.Errorf("openchoreo secrets: create %s/%s: %w", ns, name, mapSecretReferenceError(resp.StatusCode(), ErrorResponses{
			JSON400: resp.JSON400,
			JSON401: resp.JSON401,
			JSON403: resp.JSON403,
			JSON409: resp.JSON409,
			JSON500: resp.JSON500,
		}))
	}
	return resp.StatusCode(), nil
}

// update replaces the stored data of an existing secret (OpenChoreo's
// UpdateSecret replaces, it does not merge).
func (c *secretsClient) update(ctx context.Context, ns, name string, data map[string]string) error {
	resp, err := c.oc.UpdateSecretWithResponse(ctx, ns, name, gen.UpdateSecretJSONRequestBody{Data: data})
	if err != nil {
		return fmt.Errorf("openchoreo secrets: update %s/%s: %w", ns, name, err)
	}
	if resp.StatusCode() != http.StatusOK {
		return fmt.Errorf("openchoreo secrets: update %s/%s: %w", ns, name, mapSecretReferenceError(resp.StatusCode(), ErrorResponses{
			JSON400: resp.JSON400,
			JSON401: resp.JSON401,
			JSON403: resp.JSON403,
			JSON404: resp.JSON404,
			JSON500: resp.JSON500,
		}))
	}
	return nil
}

func (c *secretsClient) PatchSecret(context.Context, secretmanagersvc.SecretLocation, []byte, *secretmanagersvc.SecretMetadata) (string, error) {
	return "", secretmanagersvc.ErrNotSupported
}

// DeleteSecret removes the secret named location.SecretRefName(): OpenChoreo
// deletes its SecretReference and the stored value. A secret already gone
// is success.
func (c *secretsClient) DeleteSecret(ctx context.Context, location secretmanagersvc.SecretLocation, _ *secretmanagersvc.SecretMetadata) error {
	ns, err := location.CPNamespace()
	if err != nil {
		return err
	}
	return c.delete(ctx, ns, location.SecretRefName())
}

func (c *secretsClient) delete(ctx context.Context, ns, name string) error {
	resp, err := c.oc.DeleteSecretWithResponse(ctx, ns, name)
	if err != nil {
		return fmt.Errorf("openchoreo secrets: delete %s/%s: %w", ns, name, err)
	}
	switch resp.StatusCode() {
	case http.StatusOK, http.StatusNoContent, http.StatusNotFound:
		return nil
	}
	return fmt.Errorf("openchoreo secrets: delete %s/%s: %w", ns, name, mapSecretReferenceError(resp.StatusCode(), ErrorResponses{
		JSON400: resp.JSON400,
		JSON401: resp.JSON401,
		JSON403: resp.JSON403,
		JSON500: resp.JSON500,
	}))
}

func (c *secretsClient) GetSecret(context.Context, secretmanagersvc.SecretLocation) (*secretmanagersvc.SecretInfo, error) {
	return nil, secretmanagersvc.ErrNotSupported
}

// GetSecretWithValue reads the values of the secret named
// location.SecretRefName(), as one JSON object of key → value (the shape
// PushSecret takes).
func (c *secretsClient) GetSecretWithValue(ctx context.Context, location secretmanagersvc.SecretLocation) ([]byte, error) {
	ns, err := location.CPNamespace()
	if err != nil {
		return nil, err
	}
	name := location.SecretRefName()
	resp, err := c.oc.GetSecretWithResponse(ctx, ns, name)
	if err != nil {
		return nil, fmt.Errorf("openchoreo secrets: read %s/%s: %w", ns, name, err)
	}
	if resp.StatusCode() == http.StatusNotFound {
		return nil, fmt.Errorf("openchoreo secrets: %s/%s: %w", ns, name, secretmanagersvc.ErrSecretNotFound)
	}
	if resp.StatusCode() != http.StatusOK || resp.JSON200 == nil {
		return nil, fmt.Errorf("openchoreo secrets: read %s/%s: %w", ns, name, mapSecretReferenceError(resp.StatusCode(), ErrorResponses{
			JSON401: resp.JSON401,
			JSON403: resp.JSON403,
			JSON500: resp.JSON500,
		}))
	}
	values := map[string]string{}
	if resp.JSON200.Data != nil {
		for k, v := range *resp.JSON200.Data {
			values[k] = string(v)
		}
	}
	raw, err := json.Marshal(values)
	if err != nil {
		return nil, fmt.Errorf("openchoreo secrets: marshal %s/%s: %w", ns, name, err)
	}
	return raw, nil
}

// StoredKey reads the store key from the SecretReference OpenChoreo created
// for the secret at location. Every spec.data entry of a reference OpenChoreo
// creates points at the same key (one property per data key), so the first
// entry's key is the secret's.
func (c *secretsClient) StoredKey(ctx context.Context, location secretmanagersvc.SecretLocation) (string, error) {
	ns, err := location.CPNamespace()
	if err != nil {
		return "", err
	}
	name := location.SecretRefName()
	resp, err := c.oc.GetSecretReferenceWithResponse(ctx, ns, name)
	if err != nil {
		return "", fmt.Errorf("openchoreo secrets: read reference %s/%s: %w", ns, name, err)
	}
	if resp.StatusCode() == http.StatusNotFound {
		return "", fmt.Errorf("openchoreo secrets: %s/%s: %w", ns, name, secretmanagersvc.ErrSecretNotFound)
	}
	if resp.StatusCode() != http.StatusOK || resp.JSON200 == nil {
		return "", fmt.Errorf("openchoreo secrets: read reference %s/%s: %w", ns, name, mapSecretReferenceError(resp.StatusCode(), ErrorResponses{
			JSON401: resp.JSON401,
			JSON403: resp.JSON403,
			JSON500: resp.JSON500,
		}))
	}
	ref := secretReferenceToModel(resp.JSON200)
	if len(ref.Data) == 0 || ref.Data[0].RemoteKey == "" {
		return "", fmt.Errorf("openchoreo secrets: reference %s/%s names no store key", ns, name)
	}
	return ref.Data[0].RemoteKey, nil
}

func (c *secretsClient) Close(context.Context) error { return nil }
