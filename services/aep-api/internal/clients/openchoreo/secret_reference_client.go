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
	"errors"
	"fmt"
	"net/http"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo/gen"
	"github.com/wso2/aep/aep-api/internal/clients/secretmanagersvc"
)

// Compile-time assertion: adapter satisfies the high-level client's OC port.
var _ secretmanagersvc.OpenChoreoSecretReferenceClient = (*secretReferenceClient)(nil)

type secretReferenceClient struct {
	oc *gen.ClientWithResponses
}

// NewSecretReferenceClient builds an OpenChoreoSecretReferenceClient over the
// generated OC SecretReference API. Panics on construction failure (same
// pattern as NewGitSecretClient / NewProjectClient).
func NewSecretReferenceClient(cfg Config) secretmanagersvc.OpenChoreoSecretReferenceClient {
	oc, err := newGenClient(cfg)
	if err != nil {
		panic(fmt.Errorf("init openchoreo secret-reference client: %w", err))
	}
	return &secretReferenceClient{oc: oc}
}

func (c *secretReferenceClient) GetSecretReference(ctx context.Context, cpNS, name string) (*secretmanagersvc.SecretReference, error) {
	resp, err := c.oc.GetSecretReferenceWithResponse(ctx, cpNS, name)
	if err != nil {
		return nil, fmt.Errorf("get secret reference: %w", err)
	}
	if resp.StatusCode() != http.StatusOK || resp.JSON200 == nil {
		return nil, mapSecretReferenceError(resp.StatusCode(), ErrorResponses{
			JSON401: resp.JSON401,
			JSON403: resp.JSON403,
			JSON404: resp.JSON404,
			JSON500: resp.JSON500,
		})
	}
	return secretReferenceToModel(resp.JSON200), nil
}

func (c *secretReferenceClient) CreateSecretReference(ctx context.Context, cpNS string, req secretmanagersvc.CreateSecretReferenceRequest) (*secretmanagersvc.SecretReference, error) {
	body := buildSecretReferenceBody(req)
	resp, err := c.oc.CreateSecretReferenceWithResponse(ctx, cpNS, body)
	if err != nil {
		return nil, fmt.Errorf("create secret reference: %w", err)
	}
	if resp.StatusCode() != http.StatusCreated || resp.JSON201 == nil {
		return nil, mapSecretReferenceError(resp.StatusCode(), ErrorResponses{
			JSON400: resp.JSON400,
			JSON401: resp.JSON401,
			JSON403: resp.JSON403,
			JSON409: resp.JSON409,
			JSON500: resp.JSON500,
		})
	}
	return secretReferenceToModel(resp.JSON201), nil
}

func (c *secretReferenceClient) UpdateSecretReference(ctx context.Context, cpNS, name string, req secretmanagersvc.CreateSecretReferenceRequest) (*secretmanagersvc.SecretReference, error) {
	body := buildSecretReferenceBody(req)
	resp, err := c.oc.UpdateSecretReferenceWithResponse(ctx, cpNS, name, body)
	if err != nil {
		return nil, fmt.Errorf("update secret reference: %w", err)
	}
	if resp.StatusCode() != http.StatusOK || resp.JSON200 == nil {
		return nil, mapSecretReferenceError(resp.StatusCode(), ErrorResponses{
			JSON400: resp.JSON400,
			JSON401: resp.JSON401,
			JSON403: resp.JSON403,
			JSON404: resp.JSON404,
			JSON500: resp.JSON500,
		})
	}
	return secretReferenceToModel(resp.JSON200), nil
}

func (c *secretReferenceClient) DeleteSecretReference(ctx context.Context, cpNS, name string) error {
	resp, err := c.oc.DeleteSecretReferenceWithResponse(ctx, cpNS, name)
	if err != nil {
		return fmt.Errorf("delete secret reference: %w", err)
	}
	switch resp.StatusCode() {
	case http.StatusOK, http.StatusNoContent:
		return nil
	}
	return mapSecretReferenceError(resp.StatusCode(), ErrorResponses{
		JSON401: resp.JSON401,
		JSON403: resp.JSON403,
		JSON404: resp.JSON404,
		JSON500: resp.JSON500,
	})
}

// buildSecretReferenceBody maps CreateSecretReferenceRequest onto the gen
// SecretReference CR shape used by local SM-API / ESO:
//
//	metadata.name/namespace
//	spec.data[].{secretKey, remoteRef.{key=KVPath, property}}
//	spec.refreshInterval
//	spec.template.type=Opaque
func buildSecretReferenceBody(req secretmanagersvc.CreateSecretReferenceRequest) gen.SecretReference {
	ns := req.Namespace
	opaque := gen.Opaque
	data := make([]gen.SecretDataSource, 0, len(req.SecretKeys))
	for _, key := range req.SecretKeys {
		prop := key
		data = append(data, gen.SecretDataSource{
			SecretKey: key,
			RemoteRef: gen.RemoteReference{
				Key:      req.KVPath,
				Property: &prop,
			},
		})
	}
	spec := &gen.SecretReferenceSpec{
		Data:     data,
		Template: gen.SecretTemplate{Type: &opaque},
	}
	if req.RefreshInterval != "" {
		ri := req.RefreshInterval
		spec.RefreshInterval = &ri
	}
	return gen.SecretReference{
		Metadata: gen.ObjectMeta{
			Name:      req.Name,
			Namespace: &ns,
		},
		Spec: spec,
	}
}

func secretReferenceToModel(sr *gen.SecretReference) *secretmanagersvc.SecretReference {
	if sr == nil {
		return nil
	}
	out := &secretmanagersvc.SecretReference{
		Name: sr.Metadata.Name,
	}
	if sr.Metadata.Namespace != nil {
		out.Namespace = *sr.Metadata.Namespace
	}
	return out
}

// mapSecretReferenceError remaps openchoreo package sentinels onto
// secretmanagersvc.ErrNotFound / ErrConflict so the high-level client can
// branch with errors.Is against its own package errors.
func mapSecretReferenceError(statusCode int, errs ErrorResponses) error {
	err := handleErrorResponse(statusCode, errs)
	switch {
	case errors.Is(err, ErrNotFound):
		return secretmanagersvc.ErrNotFound
	case errors.Is(err, ErrConflict):
		return secretmanagersvc.ErrConflict
	default:
		return err
	}
}
