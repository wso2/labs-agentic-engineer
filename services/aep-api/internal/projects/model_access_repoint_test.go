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

package projects

import (
	"context"
	"errors"
	"testing"

	"github.com/wso2/aep/aep-api/internal/clients/secretmanagersvc"
	"github.com/wso2/aep/aep-api/internal/organization"
)

// repointRefs is an OpenChoreo SecretReference client holding at most the
// org's model-access reference.
type repointRefs struct {
	fakeSecretRefClient
	exists  bool
	getErr  error
	updates []secretmanagersvc.CreateSecretReferenceRequest
	ns      []string
}

func (r *repointRefs) GetSecretReference(_ context.Context, ns, name string) (*secretmanagersvc.SecretReference, error) {
	r.ns = append(r.ns, ns+"/"+name)
	if r.getErr != nil {
		return nil, r.getErr
	}
	if !r.exists {
		return nil, secretmanagersvc.ErrNotFound
	}
	return &secretmanagersvc.SecretReference{}, nil
}

func (r *repointRefs) UpdateSecretReference(_ context.Context, _, _ string, req secretmanagersvc.CreateSecretReferenceRequest) (*secretmanagersvc.SecretReference, error) {
	r.updates = append(r.updates, req)
	return &secretmanagersvc.SecretReference{}, nil
}

var repointRef = organization.SecretRefTriplet{
	Name: "acme-default-key-0000bbbb", KVPath: "user-app-secrets/wc-x/acme-default-key-0000bbbb", Property: "api-key",
}

func TestModelAccessRepointer_MovesAnExistingReferenceOntoTheNewPath(t *testing.T) {
	refs := &repointRefs{exists: true}
	if err := NewModelAccessRepointer(refs).RepointModelKey(context.Background(), "acme", repointRef); err != nil {
		t.Fatalf("RepointModelKey: %v", err)
	}
	if len(refs.ns) != 1 || refs.ns[0] != "acme/"+modelAccessSecretRefName {
		t.Fatalf("looked up %v, want the org's model-access reference in its CP namespace", refs.ns)
	}
	if len(refs.updates) != 1 {
		t.Fatalf("updates = %d, want one", len(refs.updates))
	}
	got := refs.updates[0]
	if got.KVPath != repointRef.KVPath || len(got.SecretKeys) != 1 || got.SecretKeys[0] != "api-key" ||
		got.Name != modelAccessSecretRefName || got.Namespace != "acme" || got.RefreshInterval != modelAccessSecretRefRefresh {
		t.Fatalf("update %+v, want the new path under the same name", got)
	}
}

func TestModelAccessRepointer_NoReferenceIsANoOp(t *testing.T) {
	refs := &repointRefs{}
	if err := NewModelAccessRepointer(refs).RepointModelKey(context.Background(), "acme", repointRef); err != nil || len(refs.updates) != 0 {
		t.Fatalf("err=%v updates=%d, want nothing created: the next deploy creates it", err, len(refs.updates))
	}
}

func TestModelAccessRepointer_ALookupFailureIsAnError(t *testing.T) {
	refs := &repointRefs{getErr: errors.New("oc down")}
	if err := NewModelAccessRepointer(refs).RepointModelKey(context.Background(), "acme", repointRef); err == nil || len(refs.updates) != 0 {
		t.Fatalf("err=%v updates=%d, want the failure returned and nothing written", err, len(refs.updates))
	}
}
