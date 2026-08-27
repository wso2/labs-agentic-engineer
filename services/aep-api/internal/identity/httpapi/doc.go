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

// Package httpapi embeds the identity slices into the one type the edge embeds,
// and assembles the domain from its Deps.
//
// It declares NO methods — a method here sits at depth-1 and silently shadows
// its slice (TestAggregatorsDeclareNoMethods). The assembly lives here, not in
// the identity root, so the root keeps naming no HTTP type at all.
// [Why →] services/aep-api/README.md — the composition-root convention.
package httpapi
