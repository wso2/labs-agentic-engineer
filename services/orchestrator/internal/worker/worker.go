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

// Package worker bootstraps the Temporal worker for the orchestrator.
package worker

import (
	"go.temporal.io/sdk/client"
	sdkworker "go.temporal.io/sdk/worker"
)

// New creates a worker polling the given task queue. Register workflows and
// activities on the returned worker, then call Run.
func New(c client.Client, taskQueue string) sdkworker.Worker {
	return sdkworker.New(c, taskQueue, sdkworker.Options{})
}

// Run starts the worker and blocks until an interrupt signal (SIGINT/SIGTERM).
func Run(w sdkworker.Worker) error {
	return w.Run(sdkworker.InterruptCh())
}
