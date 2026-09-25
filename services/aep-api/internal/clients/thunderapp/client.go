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

// Package thunderapp LISTs aep.wso2.com/v1alpha1 ThunderApplication CRs from
// the Kubernetes API. Plain net/http — no controller-runtime, no
// internal/clients/k8s. Not imported by delivery/validation (validation never
// mints from Thunder CR status; deploy-wait alone reads this CR).
//
// Lookup is cluster-wide by OpenChoreo labels, not namespaced GET. The
// rendered object lives in the dataplane namespace as
// r-<resourceName>-<environment>-<hash8>; the OC Resource name is on
// openchoreo.dev/resource. LIVE-VERIFIED on expense-secjson: those two
// labels uniquely identify the CR.
package thunderapp

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/wso2/aep/aep-api/internal/clients/httpx"
	"github.com/wso2/aep/aep-api/internal/clients/requests"
)

const (
	listPath = "/apis/aep.wso2.com/v1alpha1/thunderapplications"
	// Labels OpenChoreo stamps on every rendered ThunderApplication. The
	// thunder-app operator reads openchoreo.dev/resource; environment is
	// stamped by the renderedrelease-controller.
	labelResource    = "openchoreo.dev/resource"
	labelEnvironment = "openchoreo.dev/environment"
)

// Application is the deploy-wait projection of one ThunderApplication CR.
// Kept in this package so clients do not import projects (arch: clients ⊥ domains).
type Application struct {
	RedirectURIs       string
	Ready              bool
	Generation         int64
	ObservedGeneration int64
}

// Config wires the Kubernetes API endpoint the client LISTs against.
type Config struct {
	BaseURL     string
	BearerToken string
	// TokenFile is the in-cluster service-account token path. Read on each
	// request so a projected rotation is picked up. Empty when BearerToken
	// is the static KUBE_API_BEARER override.
	TokenFile string
	// CAFile is the in-cluster service-account CA (optional). Empty leaves
	// system roots; tests inject HTTPClient with the httptest cert pool.
	// When set, New fails if the file is unreadable or not PEM.
	CAFile string
	// HTTPClient overrides the default transport (tests). When nil, New
	// builds one from CAFile + httpx correlation wrap + retry.
	HTTPClient *http.Client
}

// Client LISTs ThunderApplications by OpenChoreo resource + environment labels.
type Client struct {
	baseURL   string
	bearer    string
	tokenFile string
	http      *requests.RetryableHTTPClient
}

// New builds a Client. BaseURL is required; callers that cannot resolve a
// kube API base leave the reader nil instead of calling New.
func New(cfg Config) (*Client, error) {
	if cfg.BaseURL == "" {
		return nil, fmt.Errorf("thunderapp: Config.BaseURL is required")
	}
	inner := cfg.HTTPClient
	if inner == nil {
		tr, err := tlsTransport(cfg.CAFile)
		if err != nil {
			return nil, err
		}
		inner = &http.Client{Transport: httpx.WrapTransport(tr)}
	} else if inner.Transport == nil {
		inner.Transport = httpx.WrapTransport(nil)
	}
	return &Client{
		baseURL:   strings.TrimRight(cfg.BaseURL, "/"),
		bearer:    cfg.BearerToken,
		tokenFile: cfg.TokenFile,
		// One retry with short backoff: DeploymentState already polls; a hard
		// 5xx should surface for activity retry rather than stall the poll.
		http: requests.NewRetryableHTTPClient(inner, requests.RequestRetryConfig{
			RetryAttemptsMax: 1,
			RetryWaitMin:     10 * time.Millisecond,
			RetryWaitMax:     50 * time.Millisecond,
		}),
	}, nil
}

// FindByResource fetches the ThunderApplication OpenChoreo rendered for
// resourceName in environment. (nil, nil) means none is in the cluster yet.
// More than one match is an error (the labels must uniquely identify the CR).
func (c *Client) FindByResource(ctx context.Context, resourceName, environment string) (*Application, error) {
	if c == nil {
		return nil, fmt.Errorf("thunderapp: nil client")
	}
	if resourceName == "" || environment == "" {
		return nil, fmt.Errorf("thunderapp: resourceName and environment are required")
	}
	q := url.Values{}
	q.Set("labelSelector", labelResource+"="+resourceName+","+labelEnvironment+"="+environment)
	path := listPath + "?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, fmt.Errorf("thunderapp: build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	auth, err := c.authorization()
	if err != nil {
		return nil, err
	}
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("thunderapp: LIST %s: %w", path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &HTTPError{StatusCode: resp.StatusCode, Path: path, Body: string(raw)}
	}
	var list thunderList
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, fmt.Errorf("thunderapp: decode %s: %w", path, err)
	}
	switch len(list.Items) {
	case 0:
		return nil, nil
	case 1:
		cr := list.Items[0]
		return &Application{
			RedirectURIs:       cr.Spec.RedirectURIs,
			Ready:              cr.Status.Ready,
			Generation:         cr.Metadata.Generation,
			ObservedGeneration: cr.Status.ObservedGeneration,
		}, nil
	default:
		return nil, fmt.Errorf("thunderapp: %d ThunderApplications match resource %q env %q", len(list.Items), resourceName, environment)
	}
}

// HTTPError is a non-2xx Kubernetes API response from FindByResource.
type HTTPError struct {
	StatusCode int
	Path       string
	Body       string
}

func (e *HTTPError) Error() string {
	if e == nil {
		return "thunderapp: HTTP error"
	}
	return fmt.Sprintf("thunderapp: LIST %s → %d: %s", e.Path, e.StatusCode, e.Body)
}

// IsNotFound reports a 404 from the Kubernetes API — the CRD is not served
// on this apiserver (control-plane on a split-plane install).
func IsNotFound(err error) bool {
	var he *HTTPError
	return errors.As(err, &he) && he.StatusCode == http.StatusNotFound
}

func (c *Client) authorization() (string, error) {
	if c.bearer != "" {
		return "Bearer " + c.bearer, nil
	}
	if c.tokenFile == "" {
		return "", nil
	}
	b, err := os.ReadFile(c.tokenFile)
	if err != nil {
		return "", fmt.Errorf("thunderapp: read token file: %w", err)
	}
	tok := strings.TrimSpace(string(b))
	if tok == "" {
		return "", fmt.Errorf("thunderapp: token file is empty")
	}
	return "Bearer " + tok, nil
}

type thunderList struct {
	Items []thunderCR `json:"items"`
}

type thunderCR struct {
	Metadata struct {
		Generation int64 `json:"generation"`
	} `json:"metadata"`
	Spec struct {
		RedirectURIs string `json:"redirectUris"`
	} `json:"spec"`
	Status struct {
		Ready              bool  `json:"ready"`
		ObservedGeneration int64 `json:"observedGeneration"`
	} `json:"status"`
}

func tlsTransport(caFile string) (http.RoundTripper, error) {
	if caFile == "" {
		return nil, nil
	}
	pem, err := os.ReadFile(caFile)
	if err != nil {
		return nil, fmt.Errorf("thunderapp: read CA file %s: %w", caFile, err)
	}
	if len(pem) == 0 {
		return nil, fmt.Errorf("thunderapp: CA file %s is empty", caFile)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("thunderapp: CA file %s is not valid PEM", caFile)
	}
	return &http.Transport{
		TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12},
	}, nil
}
