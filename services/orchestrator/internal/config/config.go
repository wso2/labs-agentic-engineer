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

// Package config is the single place the orchestrator reads its environment.
// Domain code never reads os.Getenv directly; it takes a Config.
package config

import (
	"os"

	"github.com/wso2/labs-agentic-engineer/packages/contracts/orchestration"
)

// Config holds the orchestrator's runtime settings. The same binary runs in
// every environment; only these values differ (see docs/design/orchestration/
// 05-configuration.md).
type Config struct {
	// TemporalHostPort is the Temporal frontend gRPC address.
	// dev: localhost:7233 · in-cluster: temporal-frontend.temporal.svc:7233
	TemporalHostPort string
	// TemporalNamespace is the Temporal namespace (dev: default · prod: aep).
	TemporalNamespace string
	// TaskQueue is the queue this worker polls; defaults to the shared constant
	// so config and code can never disagree.
	TaskQueue string
}

// Load reads the orchestrator config from the environment, applying defaults
// suitable for local dev.
func Load() Config {
	return Config{
		TemporalHostPort:  getenv("TEMPORAL_HOSTPORT", "localhost:7233"),
		TemporalNamespace: getenv("TEMPORAL_NAMESPACE", "default"),
		TaskQueue:         getenv("TEMPORAL_TASK_QUEUE", orchestration.TaskQueue),
	}
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
