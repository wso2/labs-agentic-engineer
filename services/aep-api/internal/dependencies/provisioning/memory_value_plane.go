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

package provisioning

import "sync"

// MemoryValuePlane is a process-local CatalogValuePlane. Register writes
// org env cells here so a subsequent list in the same process shows
// Registered External rows. Not durable across process restart.
// Keys are (orgID, logical name) so two orgs registering the same name
// cannot overwrite or leak each other's non-secret values.
type MemoryValuePlane struct {
	mu        sync.RWMutex
	cells     map[string]map[string][]EnvCell
	instances map[string]map[string][]ResourceInstance
}

// NewMemoryValuePlane returns an empty in-memory org value plane.
func NewMemoryValuePlane() *MemoryValuePlane {
	return &MemoryValuePlane{
		cells:     map[string]map[string][]EnvCell{},
		instances: map[string]map[string][]ResourceInstance{},
	}
}

func (p *MemoryValuePlane) EnvCells(orgID, name string) []EnvCell {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.cells == nil {
		return nil
	}
	return append([]EnvCell(nil), p.cells[orgID][name]...)
}

func (p *MemoryValuePlane) Instances(orgID, name string) []ResourceInstance {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.instances == nil {
		return nil
	}
	return append([]ResourceInstance(nil), p.instances[orgID][name]...)
}

func (p *MemoryValuePlane) PutEnvCells(orgID, name string, cells []EnvCell) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cells == nil {
		p.cells = map[string]map[string][]EnvCell{}
	}
	if p.cells[orgID] == nil {
		p.cells[orgID] = map[string][]EnvCell{}
	}
	p.cells[orgID][name] = append([]EnvCell(nil), cells...)
}

func (p *MemoryValuePlane) PutInstances(orgID, name string, instances []ResourceInstance) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.instances == nil {
		p.instances = map[string]map[string][]ResourceInstance{}
	}
	if p.instances[orgID] == nil {
		p.instances[orgID] = map[string][]ResourceInstance{}
	}
	p.instances[orgID][name] = append([]ResourceInstance(nil), instances...)
}
