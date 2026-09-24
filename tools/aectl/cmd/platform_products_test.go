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
	"errors"
	"strings"
	"testing"

	"github.com/wso2/aep/aectl/internal/products"
	"github.com/wso2/aep/aectl/internal/ui"
)

// recordingDeps returns productDeps whose install records what it was asked to
// install instead of touching a cluster.
func recordingDeps(flag string, selected []bool, confirmed bool) (productDeps, *[]string) {
	var installed []string
	return productDeps{
		productsFlag: flag,
		multiSelect: func(string, []ui.SelectItem) ([]bool, bool) {
			return selected, confirmed
		},
		install: func(_ context.Context, p products.Product, _ products.Env) error {
			installed = append(installed, p.ID)
			return nil
		},
	}, &installed
}

func TestRunProductInstall_FlagNoneInstallsNothing(t *testing.T) {
	deps, installed := recordingDeps("none", nil, false)
	if err := runProductInstall(context.Background(), products.Env{}, deps); err != nil {
		t.Fatalf("runProductInstall: %v", err)
	}
	if len(*installed) != 0 {
		t.Errorf("--products=none installed %v, want nothing", *installed)
	}
}

func TestRunProductInstall_FlagAllInstallsEveryProduct(t *testing.T) {
	deps, installed := recordingDeps("all", nil, false)
	if err := runProductInstall(context.Background(), products.Env{}, deps); err != nil {
		t.Fatalf("runProductInstall: %v", err)
	}
	if len(*installed) != len(products.Available) {
		t.Errorf("--products=all installed %d, want %d", len(*installed), len(products.Available))
	}
}

func TestRunProductInstall_UnknownIDNamesTheValidOnes(t *testing.T) {
	deps, installed := recordingDeps("nope", nil, false)
	err := runProductInstall(context.Background(), products.Env{}, deps)
	if err == nil {
		t.Fatal("unknown product ID: want an error, got nil")
	}
	if !strings.Contains(err.Error(), products.Available[0].ID) {
		t.Errorf("error %q does not name a valid ID; an operator cannot correct the flag from it", err)
	}
	if len(*installed) != 0 {
		t.Errorf("installed %v despite an unknown ID; selection must resolve fully before anything runs", *installed)
	}
}

// Esc on the selector is a decline, not an empty selection that proceeds.
func TestRunProductInstall_DeclinedSelectorInstallsNothing(t *testing.T) {
	deps, installed := recordingDeps("", []bool{true}, false)
	if err := runProductInstall(context.Background(), products.Env{}, deps); err != nil {
		t.Fatalf("runProductInstall: %v", err)
	}
	if len(*installed) != 0 {
		t.Errorf("declined selector installed %v, want nothing", *installed)
	}
}

func TestRunProductInstall_SelectorChoosesByPosition(t *testing.T) {
	selected := make([]bool, len(products.Available))
	selected[0] = true
	deps, installed := recordingDeps("", selected, true)
	if err := runProductInstall(context.Background(), products.Env{}, deps); err != nil {
		t.Fatalf("runProductInstall: %v", err)
	}
	if len(*installed) != 1 || (*installed)[0] != products.Available[0].ID {
		t.Errorf("installed %v, want [%s]", *installed, products.Available[0].ID)
	}
}

// A failing product names itself, so an operator reading the error knows which
// of several selected products left the cluster half-done.
func TestRunProductInstall_FailureNamesTheProduct(t *testing.T) {
	deps, _ := recordingDeps("all", nil, false)
	deps.install = func(context.Context, products.Product, products.Env) error {
		return errors.New("boom")
	}
	err := runProductInstall(context.Background(), products.Env{}, deps)
	if err == nil || !strings.Contains(err.Error(), products.Available[0].ID) {
		t.Errorf("error = %v, want one naming %q", err, products.Available[0].ID)
	}
}

// Every product must be selectable by ID and carry the fields the selector
// renders — a product with no Install would be silently chosen and do nothing.
func TestProductsAvailable_AreWellFormed(t *testing.T) {
	seen := map[string]bool{}
	for _, p := range products.Available {
		if p.ID == "" || p.Label == "" || p.Description == "" {
			t.Errorf("product %+v: ID, Label and Description are all required", p)
		}
		if p.Install == nil {
			t.Errorf("product %q has no Install", p.ID)
		}
		if seen[p.ID] {
			t.Errorf("product ID %q is declared twice; --products could not address either", p.ID)
		}
		seen[p.ID] = true
	}
}
