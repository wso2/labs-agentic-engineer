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

package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/viper"

	"github.com/wso2/aep/aectl/internal/products"
	"github.com/wso2/aep/aectl/internal/ui"
)

// productDeps holds the external calls runProductInstall depends on, so the
// selection and dispatch logic is testable without a cluster. Mirrors
// addonDeps.
type productDeps struct {
	multiSelect func(string, []ui.SelectItem) ([]bool, bool)
	install     func(context.Context, products.Product, products.Env) error
	// productsFlag, when non-empty, bypasses the interactive selector: "none"
	// installs nothing, "all" installs everything, otherwise a comma-separated
	// list of product IDs.
	productsFlag string
}

var defaultProductDeps = productDeps{
	multiSelect: ui.MultiSelect,
	install: func(ctx context.Context, p products.Product, env products.Env) error {
		return p.Install(ctx, env)
	},
}

// installProducts presents the additional-products selector and installs what
// was chosen. It runs after the addon step: an addon is a resource type this
// platform offers, a product is a second platform sharing the cluster, and a
// product may depend on addons being present.
func installProducts(ctx context.Context) error {
	deps := defaultProductDeps
	deps.productsFlag = initProducts
	return runProductInstall(ctx, productEnv(), deps)
}

// productEnv gathers what a product's install needs from the config this
// platform install itself ran against, so the two cannot disagree about what
// was provisioned.
func productEnv() products.Env {
	return products.Env{
		Kubeconfig:       kubeconfig,
		OrgNamespace:     ocOrgNamespace(),
		Environment:      ocPipelineSourceEnvironment(),
		ThunderNamespace: viper.GetString("thunder.namespace"),
		ThunderPublicURL: viper.GetString("thunder.public_url"),
		GatewayHost:      viper.GetString("gateway.hostname"),
	}
}

func runProductInstall(ctx context.Context, env products.Env, deps productDeps) error {
	if len(products.Available) == 0 {
		return nil
	}

	chosen, err := chooseProducts(deps)
	if err != nil || len(chosen) == 0 {
		return err
	}

	for _, p := range chosen {
		fmt.Println()
		if err := deps.install(ctx, p, env); err != nil {
			return fmt.Errorf("install %s: %w", p.ID, err)
		}
	}
	return nil
}

// chooseProducts resolves the selection from the flag or the interactive
// selector. A declined selector (Esc) returns nothing, the same as "none".
func chooseProducts(deps productDeps) ([]products.Product, error) {
	if deps.productsFlag == "" {
		items := make([]ui.SelectItem, len(products.Available))
		for i, p := range products.Available {
			items[i] = ui.SelectItem{Label: p.Label, Description: p.Description}
		}
		selected, confirmed := deps.multiSelect("Additional WSO2 products", items)
		if !confirmed {
			return nil, nil
		}
		var chosen []products.Product
		for i, p := range products.Available {
			if selected[i] {
				chosen = append(chosen, p)
			}
		}
		return chosen, nil
	}

	switch deps.productsFlag {
	case "none":
		return nil, nil
	case "all":
		return append([]products.Product(nil), products.Available...), nil
	}

	byID := make(map[string]products.Product, len(products.Available))
	known := make([]string, 0, len(products.Available))
	for _, p := range products.Available {
		byID[p.ID] = p
		known = append(known, p.ID)
	}
	var chosen []products.Product
	for _, id := range strings.Split(deps.productsFlag, ",") {
		id = strings.TrimSpace(id)
		p, ok := byID[id]
		if !ok {
			return nil, fmt.Errorf("unknown product %q — valid IDs: %s", id, strings.Join(known, ", "))
		}
		chosen = append(chosen, p)
	}
	return chosen, nil
}
