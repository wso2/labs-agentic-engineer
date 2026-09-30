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

package sreagent

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/wso2/aep/aep-api/internal/config"
	"github.com/wso2/aep/aep-api/internal/organization"
)

// reconcileInterval is the periodic pass: it heals drift no save causes, a
// `helm upgrade` that drops the hash annotation or resets replicas.
const reconcileInterval = 60 * time.Second

// Seeder applies the install-time SRE model seed for org, once, before a pass
// resolves the effective connection (organization.SreModelConnectionService.
// ApplySeed, adapted so this package need not import the seed's config-side
// values). The returned string is the outcome (organization.SeedOutcome, as
// text); a non-nil error is logged and the pass continues on whatever
// connection already applies.
type Seeder interface {
	ApplySeed(ctx context.Context, org string) (string, error)
}

// Kube is the observability plane's Kubernetes API as the reconciler uses it.
type Kube interface {
	PatchSecretData(ctx context.Context, ns, name string, data map[string][]byte) error
	PatchTemplateAnnotation(ctx context.Context, ns, deploy, key, value string) error
	Scale(ctx context.Context, ns, deploy string, replicas int32) error
	Deployment(ctx context.Context, ns, deploy string) (DeploymentState, map[string]string, error)
	Pods(ctx context.Context, ns string, selector map[string]string) ([]PodState, error)
}

// Reconciler converges the stock SRE agent of the one org cfg names on its
// effective SRE connection: the Secret's four values, the pod-template hash
// annotation (a changed hash restarts the agent on the new values) and the
// replicas (0 when the org has no connection the agent can run on).
type Reconciler struct {
	cfg      config.SREAgentConfig
	kube     Kube
	eff      func(ctx context.Context, org string) (organization.EffectiveSRE, error)
	tokens   Tokens
	seeder   Seeder
	kick     chan struct{}
	interval time.Duration
}

// NewReconciler wires the reconciler. eff is the org's effective SRE
// connection (SreModelConnectionService.EffectiveSRE); all must be non-nil.
func NewReconciler(cfg config.SREAgentConfig, kube Kube,
	eff func(ctx context.Context, org string) (organization.EffectiveSRE, error), tokens Tokens) *Reconciler {
	return &Reconciler{cfg: cfg, kube: kube, eff: eff, tokens: tokens,
		kick: make(chan struct{}, 1), interval: reconcileInterval}
}

// WithSeeder attaches the install-time seed step (Task A1): every pass calls
// it for cfg.Org before resolving the effective connection. nil (the
// default, when no seed is configured) skips the step entirely. Chainable.
func (r *Reconciler) WithSeeder(s Seeder) *Reconciler {
	r.seeder = s
	return r
}

// Run makes the boot pass, then a pass on every tick and every kick until ctx
// ends. A failed pass is logged and retried on the next one.
func (r *Reconciler) Run(ctx context.Context) {
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	r.pass(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-r.kick:
		}
		r.pass(ctx)
	}
}

// Kick asks for a pass soon, after a save that may change org's effective
// connection. Kicks for any other org are ignored; kicks that arrive before
// the pass runs coalesce into it. Never blocks.
func (r *Reconciler) Kick(org string) {
	if org != r.cfg.Org {
		return
	}
	select {
	case r.kick <- struct{}{}:
	default:
	}
}

// Status reports how org's SRE agent rollout stands (see StatusOf). ok=false
// for any org but the one this observability plane serves. It reads only: a
// token not minted yet reads as a rollout still to come.
func (r *Reconciler) Status(ctx context.Context, org string) (status, reason string, ok bool, err error) {
	if org != r.cfg.Org {
		return "", "", false, nil
	}
	eff, err := r.eff(ctx, org)
	if err != nil {
		return "", "", false, fmt.Errorf("sre agent status: effective connection: %w", err)
	}
	tok, _, err := r.tokens.Get(ctx, org)
	if err != nil {
		return "", "", false, fmt.Errorf("sre agent status: %w", err)
	}
	d := DesiredFrom(eff, tok)
	if !d.Configured {
		return string(StatusUnconfigured), "", true, nil
	}
	dep, selector, err := r.kube.Deployment(ctx, r.cfg.Namespace, r.cfg.Deployment)
	if err != nil {
		return "", "", false, fmt.Errorf("sre agent status: %w", err)
	}
	pods, err := r.kube.Pods(ctx, r.cfg.Namespace, selector)
	if err != nil {
		return "", "", false, fmt.Errorf("sre agent status: %w", err)
	}
	st, why := StatusOf(d, dep, pods)
	return string(st), why, true, nil
}

func (r *Reconciler) pass(ctx context.Context) {
	if err := r.reconcile(ctx); err != nil {
		slog.WarnContext(ctx, "sreagent.reconcile_failed", "err", err)
	}
}

// reconcile is one pass. The Secret is written before the hash annotation, so
// the pods the annotation rolls start on the new values; replicas follow.
func (r *Reconciler) reconcile(ctx context.Context) error {
	org, ns := r.cfg.Org, r.cfg.Namespace
	if r.seeder != nil {
		if _, err := r.seeder.ApplySeed(ctx, org); err != nil {
			slog.WarnContext(ctx, "sreagent.seed_failed", "org", org, "err", err)
		}
	}
	eff, err := r.eff(ctx, org)
	if err != nil {
		return fmt.Errorf("effective connection: %w", err)
	}
	tok, err := r.tokens.Ensure(ctx, org)
	if err != nil {
		return err
	}
	d := DesiredFrom(eff, tok)
	dep, _, err := r.kube.Deployment(ctx, ns, r.cfg.Deployment)
	if err != nil {
		return err
	}
	if hash := d.Hash(); dep.TemplateHash != hash {
		if err := r.kube.PatchSecretData(ctx, ns, r.cfg.Secret, d.SecretData()); err != nil {
			return err
		}
		if err := r.kube.PatchTemplateAnnotation(ctx, ns, r.cfg.Deployment, HashAnnotation, hash); err != nil {
			return err
		}
		slog.InfoContext(ctx, "sreagent.pushed", "org", org, "source", string(eff.Source), "configured", d.Configured)
	}
	if want := d.Replicas(); dep.Replicas != want {
		if err := r.kube.Scale(ctx, ns, r.cfg.Deployment, want); err != nil {
			return err
		}
		slog.InfoContext(ctx, "sreagent.scaled", "org", org, "replicas", want)
	}
	return nil
}
