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

package delivery

// progress.go — proof of life from deep inside long work. The run supervisor
// heartbeats its long activities (delivery/run), but the facts that show the
// work is MOVING (each event of a Plan turn) are seen by the slice doing it
// (delivery/task), which must not know Temporal and cannot import the run
// package. The activity installs a beat on the context; the work reports.

import "context"

type progressKey struct{}

// WithProgress returns ctx carrying beat, which ReportProgress calls.
func WithProgress(ctx context.Context, beat func()) context.Context {
	return context.WithValue(ctx, progressKey{}, beat)
}

// ReportProgress says the work under ctx just moved. A no-op when nothing
// listens.
func ReportProgress(ctx context.Context) {
	if beat, ok := ctx.Value(progressKey{}).(func()); ok && beat != nil {
		beat()
	}
}
