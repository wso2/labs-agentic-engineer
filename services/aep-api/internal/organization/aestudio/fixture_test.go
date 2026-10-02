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

package aestudio

// fixture_test.go — the Service over an in-memory OpenChoreo that behaves
// like the real one where the converge depends on it: a parameters change
// cuts a release, a binding becomes Ready with its outputs, and fillDefaults
// adds what OpenChoreo adds on its own.

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/clients/secretmanagersvc"
	"github.com/wso2/aep/aep-api/internal/config"
	"github.com/wso2/aep/aep-api/internal/gen"
	"github.com/wso2/aep/aep-api/internal/organization"
	"github.com/wso2/aep/aep-api/internal/platform/auth"
	"github.com/wso2/aep/aep-api/internal/platform/modelconn"
)

var ctx = context.Background()

const testOU = "6f1c2a3b-4d5e-4f60-8a71-92b3c4d5e6f7"

// userCtx is a request's context: it carries the caller's user JWT.
func userCtx() context.Context { return auth.WithAuthToken(context.Background(), "user-jwt") }

func anthropicConn(model string) modelconn.Connection {
	return modelconn.Connection{Format: modelconn.FormatAnthropic, BaseURL: modelconn.AnthropicBaseURL,
		Host: modelconn.AnthropicHost, Model: model, AuthScheme: modelconn.AuthXAPIKey}
}

// fakeOC is one OpenChoreo org namespace. Every call is logged as "VERB
// kind name"; secret-reference reads are not logged.
type fakeOC struct {
	mu          sync.Mutex
	calls       []string
	violations  []string
	delay       time.Duration
	project     bool
	targetErr   error
	env         string
	prb         bool
	rt          *openchoreo.ResourceType
	res         *openchoreo.Resource
	release     string
	cuts        int
	rrb         *openchoreo.ResourceReleaseBinding
	readyOnBind bool
	refs        map[string]*secretmanagersvc.SecretReference
}

func (o *fakeOC) log(view ocView, c context.Context, call string, write bool) {
	o.calls = append(o.calls, call)
	if write && !view.converge {
		o.violations = append(o.violations, "status client wrote: "+call)
	}
	if write && !auth.IsServiceIdentity(c) {
		o.violations = append(o.violations, "write not as aep-api's own identity: "+call)
	}
	if c.Err() != nil {
		o.violations = append(o.violations, "call on a cancelled context: "+call)
	}
}

// writes counts the logged writes.
func (o *fakeOC) writes() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	n := 0
	for _, c := range o.calls {
		if strings.HasPrefix(c, "POST ") || strings.HasPrefix(c, "PUT ") || strings.HasPrefix(c, "PRB ") {
			n++
		}
	}
	return n
}

func (o *fakeOC) count(call string) int {
	o.mu.Lock()
	defer o.mu.Unlock()
	n := 0
	for _, c := range o.calls {
		if c == call {
			n++
		}
	}
	return n
}

func (o *fakeOC) resetCalls() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.calls = nil
}

// cut is the controller cutting a new ResourceRelease.
func (o *fakeOC) cut() {
	o.cuts++
	o.release = fmt.Sprintf("ae-studio-%d", o.cuts)
	o.res.Status = &openchoreo.ResourceStatus{LatestRelease: &openchoreo.ResourceLatestRelease{Name: o.release}}
}

// fillDefaults adds what OpenChoreo adds on its own: schema defaults (and a
// key a future schema could default), labels, annotations, a dropped empty
// array, the binding's retain policy.
func (o *fakeOC) fillDefaults() {
	o.mu.Lock()
	defer o.mu.Unlock()
	labels := map[string]string{"openchoreo.dev/namespace": "default", "openchoreo.dev/project": ProjectName}
	o.rt.Metadata.Labels = labels
	o.rt.Metadata.Annotations["openchoreo.dev/observed"] = "1"
	o.res.Metadata.Labels = labels
	o.rrb.Metadata.Labels = labels
	var p map[string]any
	_ = json.Unmarshal(o.res.Spec.Parameters, &p)
	p["x-defaulted"] = "by-schema"
	agent := p["secrets"].(map[string]any)["designAgent"].(map[string]any)
	if d, _ := agent["data"].([]any); len(d) == 0 {
		delete(agent, "data")
	}
	o.res.Spec.Parameters, _ = json.Marshal(p)
	var e map[string]any
	_ = json.Unmarshal(o.rrb.Spec.ResourceTypeEnvironmentConfigs, &e)
	e["x-defaulted"] = true
	e["storage"].(map[string]any)["unknown"] = "default"
	o.rrb.Spec.ResourceTypeEnvironmentConfigs, _ = json.Marshal(e)
	o.rrb.Spec.RetainPolicy = "Delete"
}

func (o *fakeOC) setReady(ready bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.readyOnBind = ready
	if o.rrb != nil {
		o.rrb.Status = bindingStatus(ready)
	}
}

func bindingStatus(ready bool) *openchoreo.ResourceReleaseBindingStatus {
	st := "False"
	if ready {
		st = "True"
	}
	return &openchoreo.ResourceReleaseBindingStatus{
		Conditions: []openchoreo.OCCondition{{Type: "Ready", Status: st}},
		Outputs: []openchoreo.ResolvedOutput{
			{Name: "designUrl", Value: "http://default-ae-design-agent.gw"},
			{Name: "collabUrl", Value: "ws://default-ae-collab.gw"},
			{Name: "toolsUrl", Value: "http://default-ae-studio-tools.gw"},
		},
	}
}

func clone[T any](v *T) *T {
	if v == nil {
		return nil
	}
	raw, _ := json.Marshal(v)
	out := new(T)
	_ = json.Unmarshal(raw, out)
	return out
}

// ocView is one client set over the fake: the status clients (reads only)
// or the converge clients.
type ocView struct {
	openchoreo.ResourceClient // unused methods: a call panics
	o                         *fakeOC
	converge                  bool
}

func (v ocView) do(c context.Context, call string, write bool, fn func() error) error {
	if v.o.delay > 0 {
		time.Sleep(v.o.delay)
	}
	v.o.mu.Lock()
	defer v.o.mu.Unlock()
	v.o.log(v, c, call, write)
	return fn()
}

// verb is POST when the object does not exist yet, else PUT.
func (v ocView) verb(missing func() bool) string {
	v.o.mu.Lock()
	defer v.o.mu.Unlock()
	if missing() {
		return "POST"
	}
	return "PUT"
}

func jsonEqual(a, b json.RawMessage) bool {
	var x, y any
	_ = json.Unmarshal(a, &x)
	_ = json.Unmarshal(b, &y)
	return reflect.DeepEqual(x, y)
}

func notFound(what string) error { return fmt.Errorf("%w: %s", openchoreo.ErrNotFound, what) }

func (v ocView) GetProject(c context.Context, _, name string) (p *gen.Project, err error) {
	err = v.do(c, "GET project "+name, false, func() error {
		if !v.o.project {
			return notFound("project")
		}
		p = &gen.Project{Name: name, DeploymentPipeline: "default"}
		return nil
	})
	return p, err
}

func (v ocView) CreateProject(c context.Context, _ string, req *gen.CreateProjectRequest) (*gen.Project, error) {
	err := v.do(c, "POST project "+req.Name, true, func() error { v.o.project = true; return nil })
	return &gen.Project{Name: req.Name}, err
}

func (v ocView) Resolve(c context.Context, _, _ string) (env string, err error) {
	err = v.do(c, "resolve write target", false, func() error {
		switch {
		case v.o.targetErr != nil:
			return v.o.targetErr
		case !v.o.project:
			return fmt.Errorf("resolve write target: %w", notFound("project"))
		}
		env = v.o.env
		return nil
	})
	return env, err
}

func (v ocView) EnsureProjectReleaseBinding(c context.Context, _, project, env string) error {
	if v.o.prb {
		return v.do(c, "GET prb "+project+"-"+env, false, func() error { return nil })
	}
	return v.do(c, "PRB "+project+"-"+env, true, func() error { v.o.prb = true; return nil })
}

func (v ocView) GetResourceType(c context.Context, _, name string) (rt *openchoreo.ResourceType, err error) {
	err = v.do(c, "GET resourcetype "+name, false, func() error {
		if v.o.rt == nil {
			return notFound("resourcetype")
		}
		rt = clone(v.o.rt)
		return nil
	})
	return rt, err
}

func (v ocView) EnsureResourceType(c context.Context, _ string, rt *openchoreo.ResourceType) (out *openchoreo.ResourceType, err error) {
	err = v.do(c, "POST resourcetype "+rt.Metadata.Name, true, func() error {
		v.o.rt = clone(rt)
		out = clone(rt)
		return nil
	})
	return out, err
}

func (v ocView) UpdateResourceType(c context.Context, _ string, rt *openchoreo.ResourceType) (out *openchoreo.ResourceType, err error) {
	err = v.do(c, "PUT resourcetype "+rt.Metadata.Name, true, func() error {
		v.o.rt = clone(rt)
		out = clone(rt)
		return nil
	})
	return out, err
}

func (v ocView) GetResource(c context.Context, _, name string) (r *openchoreo.Resource, err error) {
	err = v.do(c, "GET resource "+name, false, func() error {
		if v.o.res == nil {
			return notFound("resource")
		}
		r = clone(v.o.res)
		return nil
	})
	return r, err
}

func (v ocView) ApplyResource(c context.Context, _ string, r *openchoreo.Resource) (out *openchoreo.Resource, err error) {
	verb := v.verb(func() bool { return v.o.res == nil })
	err = v.do(c, verb+" resource "+r.Metadata.Name, true, func() error {
		if v.o.res == nil {
			v.o.res = clone(r)
			out = clone(r) // no release yet at create
			v.o.cut()
			return nil
		}
		prior := v.o.res.Status
		changed := !jsonEqual(r.Spec.Parameters, v.o.res.Spec.Parameters)
		v.o.res.Spec = *clone(&r.Spec)
		out = clone(v.o.res)
		out.Status = clone(prior)
		if changed {
			v.o.cut()
		}
		return nil
	})
	return out, err
}

func (v ocView) GetBinding(c context.Context, _, name string) (b *openchoreo.ResourceReleaseBinding, err error) {
	err = v.do(c, "GET rrb "+name, false, func() error {
		b = clone(v.o.rrb)
		return nil
	})
	return b, err
}

func (v ocView) EnsureBinding(c context.Context, _ string, b *openchoreo.ResourceReleaseBinding) (out *openchoreo.ResourceReleaseBinding, err error) {
	verb := v.verb(func() bool { return v.o.rrb == nil })
	err = v.do(c, verb+" rrb "+b.Metadata.Name+" pin="+b.Spec.ResourceRelease, true, func() error {
		v.o.rrb = clone(b)
		v.o.rrb.Status = bindingStatus(v.o.readyOnBind)
		out = clone(v.o.rrb)
		return nil
	})
	return out, err
}

func (v ocView) GetSecretReference(_ context.Context, _, name string) (*secretmanagersvc.SecretReference, error) {
	v.o.mu.Lock()
	defer v.o.mu.Unlock()
	ref, ok := v.o.refs[name]
	if !ok {
		return nil, secretmanagersvc.ErrNotFound
	}
	return clone(ref), nil
}

func (v ocView) clients() OC {
	return OC{Projects: v, Cells: v, Targets: v, Resources: v, SecretRefs: v}
}

// fixture is the Service for org "default" over a fakeOC.
type fixture struct {
	t     *testing.T
	oc    *fakeOC
	svc   *Service
	org   *fakeOrg
	clock *fakeClock
}

type fakeOrg struct {
	mu      sync.Mutex
	secrets map[organization.OrgSecret]string
	conn    *modelconn.Connection
	login   string
}

func (f *fakeOrg) List(_ context.Context, _ string) ([]organization.OrgSecretRef, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []organization.OrgSecretRef
	for _, s := range organization.OrgSecrets() {
		if n, ok := f.secrets[s]; ok {
			out = append(out, organization.OrgSecretRef{Secret: s, Name: n})
		}
	}
	return out, nil
}

func (f *fakeOrg) GetByName(_ context.Context, name string) (*organization.Organization, error) {
	ou := uuid.MustParse(testOU)
	return &organization.Organization{Name: name, ThunderOrgUUID: &ou}, nil
}

func (f *fakeOrg) GetProfileByOrgID(_ context.Context, org string) (*organization.OrganizationIDPProfile, error) {
	return &organization.OrganizationIDPProfile{OrgID: org, StudioClientID: "ae-studio-" + org}, nil
}

func (f *fakeOrg) Connection(_ context.Context, _ string) (modelconn.Connection, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.conn == nil {
		return modelconn.Connection{}, false, nil
	}
	return *f.conn, true, nil
}

func (f *fakeOrg) Status(_ context.Context, org string) (*organization.Projection, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.login == "" {
		return nil, &organization.NotFoundError{What: "org_credentials." + org}
	}
	return &organization.Projection{GitHubLogin: f.login}, nil
}

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func testConfig() config.AEStudioConfig {
	var c config.AEStudioConfig
	c.Images.DesignAgent, c.Images.Collab, c.Images.StudioTools = "ae-design-agent:1", "ae-collab:1", "ae-studio-tools:1"
	c.GatewayHost, c.PublicScheme, c.PublicPortSuffix, c.ListenerName = "openchoreoapis.localhost", "http", ":19080", "http"
	c.ConsoleOrigins = []string{"http://console.localhost", "http://localhost:8090"}
	c.IDP.Issuer, c.IDP.JWKSURL, c.IDP.TokenURL = "http://thunder", "http://thunder/oauth2/jwks", "http://thunder/oauth2/token"
	c.IDP.UserAudiences = []string{"aep-console-client"}
	c.AEPAPIBaseURL, c.InternalClientID = "http://aep-api:9090", "ae-studio-internal-client"
	c.Storage.SizeLimit, c.Storage.EphemeralRequest, c.Storage.BudgetBytes = "3Gi", "1Gi", 2147483648
	c.ExtraEgress = json.RawMessage(`[{"to":[{"podSelector":{}}]}]`)
	return c
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	oc := &fakeOC{env: "development", readyOnBind: true, refs: map[string]*secretmanagersvc.SecretReference{}}
	org := &fakeOrg{secrets: map[organization.OrgSecret]string{}}
	clock := &fakeClock{t: time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)}
	svc := New(Deps{
		Config: testConfig(), OrgSecrets: org, Orgs: org, Profiles: org, Connections: org, GitHub: org,
		StatusOC:   ocView{o: oc}.clients(),
		ConvergeOC: ocView{o: oc, converge: true}.clients(),
	})
	svc.now = clock.now
	f := &fixture{t: t, oc: oc, svc: svc, org: org, clock: clock}
	t.Cleanup(func() {
		f.waitIdle(t)
		oc.mu.Lock()
		defer oc.mu.Unlock()
		for _, v := range oc.violations {
			t.Error(v)
		}
	})
	return f
}

// withRefs sets these org secrets, each with a reference whose spec.data
// holds one entry per data key (the PAT also as password).
func (f *fixture) withRefs(refs map[organization.OrgSecret]string) *fixture {
	for s, name := range refs {
		f.withRef(s, name)
	}
	return f
}

func (f *fixture) withRef(s organization.OrgSecret, name string) *fixture {
	f.org.mu.Lock()
	f.org.secrets[s] = name
	f.org.mu.Unlock()
	ref := &secretmanagersvc.SecretReference{Namespace: "default", Name: name}
	keys := s.Keys()
	if s == organization.OrgSecretGitHubPAT {
		keys = append(keys, "password")
	}
	for _, k := range keys {
		ref.Data = append(ref.Data, secretmanagersvc.SecretReferenceData{SecretKey: k, RemoteKey: "user-app-secrets/ns/" + name, Property: k})
	}
	f.oc.mu.Lock()
	f.oc.refs[name] = ref
	f.oc.mu.Unlock()
	return f
}

func (f *fixture) withoutRef(s organization.OrgSecret) *fixture {
	f.org.mu.Lock()
	defer f.org.mu.Unlock()
	delete(f.org.secrets, s)
	return f
}

func (f *fixture) withAllRefs() *fixture {
	return f.withRefs(map[organization.OrgSecret]string{
		organization.OrgSecretGitHubPAT: "default-github-pat-aaaa0001", organization.OrgSecretGitHubWebhookSecret: "default-github-webhook-secret-aaaa0002",
		organization.OrgSecretPublisherClient: "default-ae-publisher-client-aaaa0003", organization.OrgSecretStudioClient: "default-ae-studio-client-aaaa0004",
	})
}

func (f *fixture) withConnection(c modelconn.Connection) *fixture {
	f.org.mu.Lock()
	defer f.org.mu.Unlock()
	f.org.conn = &c
	return f
}

func (f *fixture) withGitHubLogin(login string) *fixture {
	f.org.mu.Lock()
	defer f.org.mu.Unlock()
	f.org.login = login
	return f
}

func (f *fixture) withoutConfig(env string) *fixture {
	unset := map[string]func(*config.AEStudioConfig){
		"AE_STUDIO_IMAGE_COLLAB": func(c *config.AEStudioConfig) { c.Images.Collab = "" },
		"AE_STUDIO_GATEWAY_HOST": func(c *config.AEStudioConfig) { c.GatewayHost = "" },
	}
	unset[env](&f.svc.cfg)
	return f
}

func (f *fixture) withImage(collab string) *fixture {
	f.svc.cfg.Images.Collab = collab
	return f
}

func (f *fixture) withWriteTargetErr(err error) *fixture {
	f.oc.mu.Lock()
	defer f.oc.mu.Unlock()
	f.oc.targetErr = err
	return f
}

func (f *fixture) withLiveHash(h string) *fixture {
	f.oc.mu.Lock()
	defer f.oc.mu.Unlock()
	f.oc.rt.Metadata.Annotations = maps.Clone(f.oc.rt.Metadata.Annotations)
	f.oc.rt.Metadata.Annotations[templateHashAnnotation] = h
	return f
}

func (f *fixture) withNewRelease() *fixture {
	f.oc.mu.Lock()
	defer f.oc.mu.Unlock()
	f.oc.cut()
	return f
}

func (f *fixture) withReady(ready bool) *fixture {
	f.oc.setReady(ready)
	return f
}

func (f *fixture) slowOC(d time.Duration) *fixture {
	f.oc.delay = d
	return f
}

// converged runs a converge to the end and forgets its calls.
func (f *fixture) converged() *fixture {
	f.svc.Trigger(userCtx(), "default")
	f.waitConverged(f.t)
	f.oc.resetCalls()
	return f
}

// waitIdle waits for every converge of "default" to end.
func (f *fixture) waitIdle(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for f.svc.busy("default") {
		if time.Now().After(deadline) {
			t.Fatal("converge still running after 10s")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// waitConverged waits for the converges to end and fails the test if the
// last one failed.
func (f *fixture) waitConverged(t *testing.T) {
	t.Helper()
	f.waitIdle(t)
	f.svc.mu.Lock()
	_, failed := f.svc.failures["default"]
	f.svc.mu.Unlock()
	if failed {
		t.Fatalf("converge failed; calls %v", f.oc.calls)
	}
}
