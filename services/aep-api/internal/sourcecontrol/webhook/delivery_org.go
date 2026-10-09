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

package webhook

import "context"

// The delivery org: the org a delivery was accepted for (the ingest caller's
// verified org, or the GitHub App routing's), carried on every handler run's
// context. A handler that resolves a repository by its GitHub full name looks
// only in this org, so a payload naming another org's repository resolves to
// nothing. The Replayer re-runs a delivery outside any request; its run gets
// the org stored on the delivery row the same way.

type deliveryOrgKey struct{}

// WithDeliveryOrg returns ctx carrying org as the delivery org.
func WithDeliveryOrg(ctx context.Context, org string) context.Context {
	return context.WithValue(ctx, deliveryOrgKey{}, org)
}

// DeliveryOrg is the delivery org ctx carries; false when it carries none (or
// an empty one).
func DeliveryOrg(ctx context.Context) (string, bool) {
	org, _ := ctx.Value(deliveryOrgKey{}).(string)
	return org, org != ""
}
