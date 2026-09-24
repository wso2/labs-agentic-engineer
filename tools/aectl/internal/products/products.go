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

// Package products declares additional WSO2 products that can be installed
// onto a cluster `aectl platform install` has already provisioned.
//
// A product is not an addon. An addon is data — a Helm release and some
// manifests the caller applies — and the shape in internal/addons reflects
// that. A product is a procedure: it installs several charts, takes ownership
// of objects another installer created, and composes bootstrap documents that
// exist once per cluster. So a Product carries an Install function rather than
// a manifest list, and the selector is the only thing the two share.
//
// To add a product, append an entry to Available with its own Install.
package products

import "context"

// Env is what a product's Install needs from the running `aectl platform
// install`, passed rather than re-derived so a product cannot disagree with
// the platform install that preceded it about what was provisioned.
type Env struct {
	// Kubeconfig is the --kubeconfig value, empty for the ambient context.
	Kubeconfig string
	// OrgNamespace is the k8s namespace of the one org AEP is provisioned for
	// (oc.default_org_namespace).
	OrgNamespace string
	// Environment is the OpenChoreo Environment AEP provisions into — the
	// source of DeploymentPipeline/default's promotion graph
	// (oc.pipeline_source_environment).
	Environment string
	// ThunderNamespace is where the platform identity provider runs
	// (thunder.namespace). A product that has to compose with its bootstrap
	// documents resolves the Helm release from the namespace rather than
	// assuming a name, since the release is not a config key.
	ThunderNamespace string
	// ThunderPublicURL is the IdP's externally reachable URL (thunder.public_url).
	// It is what the platform's own clients send as the OAuth resource
	// indicator, so a product that redeclares the System resource server must
	// derive its identifier from this and nothing else.
	ThunderPublicURL string
	// GatewayHost is the ClusterDataPlane's external hostname
	// (gateway.hostname) — component routes are built from it, so a product
	// that installs per-environment gateways must override its chart default
	// onto this.
	GatewayHost string
}

// Product is an additional WSO2 product installable beside AEP.
type Product struct {
	ID          string
	Label       string
	Description string
	// Install performs the whole install. It owns its own progress output and
	// returns the first error that leaves the cluster in a state the operator
	// has to look at; a product that declines to run (a prerequisite absent,
	// say) says so on stdout and returns nil.
	Install func(ctx context.Context, env Env) error
}

// Available is the ordered list shown in the "Additional WSO2 products"
// selector after the addon step.
var Available = []Product{
	{
		ID:          "agent-manager",
		Label:       "WSO2 Agent Manager",
		Description: "Agent management platform — shares this cluster's OpenChoreo, identity provider and environment",
		Install:     installAgentManager,
	},
}
