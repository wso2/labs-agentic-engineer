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

package aestudiotools

// endpoint.go — where an org's ae-studio-tools answers (R13): its internal
// API root and the OU id it pins, cached for about 30 s. The composition root
// resolves them from the org's AE Studio status, so this package stays free
// of domain imports.

import (
	"context"
	"sync"
	"time"
)

// endpointTTL is how long a resolved Target is reused.
const endpointTTL = 30 * time.Second

// InternalAPIPath is the root of ae-studio-tools' machine API under its
// public URL (the gateway routes /internal/v1 to the pod).
const InternalAPIPath = "/internal/v1"

// Target is what the org's pod pins: the root of its internal API (its tools
// URL + InternalAPIPath) and the OU id every call sends as X-Impersonate-Org
// (never the OpenChoreo org name).
type Target struct{ BaseURL, ImpersonateOrg string }

// Endpoints resolves an org's Target. ErrAEStudioAbsent when the org has no
// AE Studio; ErrAEStudioUnavailable when it is provisioning, failed or its
// status cannot be read.
type Endpoints interface {
	Resolve(ctx context.Context, org string) (Target, error)
}

// endpointCache keeps each org's resolved Target for endpointTTL. Refusals
// are not cached, and a transport error drops the org's entry.
type endpointCache struct {
	next Endpoints
	now  func() time.Time

	mu      sync.Mutex
	targets map[string]cachedTarget
}

type cachedTarget struct {
	target  Target
	expires time.Time
}

func newEndpointCache(next Endpoints) *endpointCache {
	return &endpointCache{next: next, now: time.Now, targets: map[string]cachedTarget{}}
}

//deadcode:keep wired in Task 3.17
func (c *endpointCache) resolve(ctx context.Context, org string) (Target, error) {
	c.mu.Lock()
	hit, ok := c.targets[org]
	c.mu.Unlock()
	if ok && c.now().Before(hit.expires) {
		return hit.target, nil
	}
	t, err := c.next.Resolve(ctx, org)
	if err != nil {
		return Target{}, err
	}
	c.mu.Lock()
	c.targets[org] = cachedTarget{target: t, expires: c.now().Add(endpointTTL)}
	c.mu.Unlock()
	return t, nil
}

// drop forgets the org's Target, so the next call resolves it again.
//
//deadcode:keep wired in Task 3.17
func (c *endpointCache) drop(org string) {
	c.mu.Lock()
	delete(c.targets, org)
	c.mu.Unlock()
}
