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

// local-secret-manager-api is a docker-compose-only stand-in for the
// wso2cloud secret-manager-api (sm-api). It exists so the public
// app-factory repo can run the full local setup WITHOUT a private
// `wso2cloud` checkout — the real sm-api builds from
// `../../wso2cloud/backend/secret-manager-api`, which OSS users don't
// have. Mirrors the `deployments/local-cluster-gateway-proxy` precedent.
//
// CONTRACT — mirrored from the real service (keep in sync if it changes):
//   - wso2cloud/backend/secret-manager-api/internal/service/service.go
//       (generateSecretRefName, buildSecretReference, Create/List/Delete,
//        managedByLabel/Value, GVR, sanitizeDNSName)
//   - wso2cloud/backend/secret-manager-api/internal/vault/eso.go
//       (VaultPath = "{prefix}/{namespace}/{secretRefName}")
//   - wso2cloud/backend/secret-manager-api/internal/auth/jwt.go
//       (GenerateNamespaceName — namespace derived from the JWT `ouId`)
//   - wso2cloud/backend/secret-manager-api/internal/types/types.go
//       (request/response JSON shapes)
//
// The asdlc-service client (asdlc-service/clients/secretmanagersvc) calls
// only CreateSecret + DeleteSecret, so this stub implements exactly the
// routes those exercise: POST /secrets, GET /secrets?labelSelector=
// (used by DeleteSecret's resolveSecretID), DELETE /secrets/{id}.
// GET /secrets/{id} and PATCH are intentionally omitted.
//
// DELIBERATE LOCAL SIMPLIFICATIONS vs. the real service:
//   - Writes OpenBao over plain HTTP with the root token instead of the
//     ESO Go vault provider + k8s-SA auth. The RESULT — KV-v2 data at the
//     same logical path — is identical for the per-run ExternalSecret that
//     reads it, so this is invisible downstream.
//   - Decode-only JWT (no signature verification). Safe for the
//     single-tenant local dev stack: `ouId` only scopes the namespace and
//     there is no second org. Do NOT reuse this stub multi-tenant.
//   - Skips ValidateUserLabels (reserved-prefix rejection) — the BFF never
//     sends reserved labels.
package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// Mirrors the real service's constants (service.go). Do not drift.
const (
	managedByLabel      = "cloud.wso2.com/managed-by"
	managedByValue      = "wso2cloud-secret-manager"
	secretRefAPIVersion = "openchoreo.dev/v1alpha1"
	secretRefKind       = "SecretReference"
	maxNamePartLength   = 40
)

var secretReferenceGVR = schema.GroupVersionResource{
	Group:    "openchoreo.dev",
	Version:  "v1alpha1",
	Resource: "secretreferences",
}

type config struct {
	listenAddr      string
	openbaoAddr     string
	openbaoToken    string
	vaultPathPrefix string
	secretRefPrefix string
	refreshInterval string
}

func loadConfig() config {
	return config{
		listenAddr:      envOr("LISTEN_ADDR", ":8082"),
		openbaoAddr:     strings.TrimRight(envOr("OPENBAO_ADDR", "http://host.docker.internal:8200"), "/"),
		openbaoToken:    envOr("OPENBAO_TOKEN", "root"),
		vaultPathPrefix: envOr("VAULT_PATH_PREFIX", "user-app-secrets"),
		secretRefPrefix: envOr("SECRET_REF_PREFIX", "cred-"),
		refreshInterval: envOr("SECRET_REF_REFRESH_INTERVAL", "15s"),
	}
}

type server struct {
	cfg  config
	dyn  dynamic.Interface
	http *http.Client
}

// ───────────────────────── request / response shapes ─────────────────────────
// Mirror wso2cloud/.../internal/types/types.go exactly.

type createRequest struct {
	Metadata struct {
		Name   string            `json:"name"`
		Labels map[string]string `json:"labels,omitempty"`
	} `json:"metadata"`
	Spec struct {
		Data map[string]string `json:"data"`
	} `json:"spec"`
}

type secretResponse struct {
	Metadata struct {
		ID                string            `json:"id"`
		Namespace         string            `json:"namespace"`
		Labels            map[string]string `json:"labels,omitempty"`
		CreationTimestamp time.Time         `json:"creationTimestamp"`
	} `json:"metadata"`
	Spec struct {
		Keys                []string `json:"keys"`
		SecretReferenceName string   `json:"secretReferenceName"`
	} `json:"spec"`
}

type listResponse struct {
	Items []secretResponse `json:"items"`
}

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: parseLogLevel(os.Getenv("LOG_LEVEL"))})))
	cfg := loadConfig()

	restCfg, err := loadKubeRESTConfig()
	if err != nil {
		slog.Error("kubeconfig load failed", "error", err)
		os.Exit(1)
	}
	dyn, err := dynamic.NewForConfig(restCfg)
	if err != nil {
		slog.Error("dynamic client init failed", "error", err)
		os.Exit(1)
	}

	s := &server{cfg: cfg, dyn: dyn, http: &http.Client{Timeout: 15 * time.Second}}
	slog.Info("local-secret-manager-api starting",
		"listenAddr", cfg.listenAddr,
		"openbao", cfg.openbaoAddr,
		"kubeAPI", restCfg.Host,
		"vaultPathPrefix", cfg.vaultPathPrefix)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("POST /secrets", s.handleCreate)
	mux.HandleFunc("GET /secrets", s.handleList)
	mux.HandleFunc("DELETE /secrets/{id}", s.handleDelete)

	srv := &http.Server{
		Addr:         cfg.listenAddr,
		Handler:      withRequestLogging(mux),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}()
	<-ctx.Done()
	slog.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
}

// ───────────────────────────────── handlers ─────────────────────────────────

func (s *server) handleCreate(w http.ResponseWriter, r *http.Request) {
	ns, ok := s.namespaceFromRequest(w, r)
	if !ok {
		return
	}
	var req createRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if req.Metadata.Name == "" || len(req.Spec.Data) == 0 {
		writeError(w, http.StatusBadRequest, "metadata.name and spec.data are required")
		return
	}

	secretRefName := s.generateSecretRefName(req.Metadata.Name)
	vaultPath := fmt.Sprintf("%s/%s/%s", s.cfg.vaultPathPrefix, ns, secretRefName)

	// 1) Push to OpenBao. Each map key becomes a distinct KV-v2 field
	//    (one vault property per key) — the publisher secret is 2-key
	//    (client_id, client_secret) and the dispatcher reads each as a
	//    separate remoteRef.property.
	if err := s.vaultWrite(r.Context(), vaultPath, req.Spec.Data); err != nil {
		slog.Error("vault write failed", "path", vaultPath, "error", err)
		writeError(w, http.StatusInternalServerError, "vault write failed: "+err.Error())
		return
	}

	// 2) Create the SecretReference CR. On failure, clean up the vault
	//    entry we just wrote (mirror the real service — avoids orphaned
	//    vault data when OC's mutating webhook 502s locally).
	keys := sortedKeys(req.Spec.Data)
	cr := s.buildSecretReference(secretRefName, ns, vaultPath, keys, req.Metadata.Labels)
	created, err := s.dyn.Resource(secretReferenceGVR).Namespace(ns).Create(r.Context(), cr, metav1.CreateOptions{})
	if err != nil {
		if apierrors.IsAlreadyExists(err) {
			// Same generated name already present (uuid collision — vanishingly
			// rare). Treat as success: vault already holds the latest data.
			// Fetch the live object so the response carries the real
			// creationTimestamp (cr is the unsubmitted local copy).
			slog.Warn("SecretReference already exists, treating create as idempotent", "name", secretRefName)
			created, err = s.dyn.Resource(secretReferenceGVR).Namespace(ns).Get(r.Context(), secretRefName, metav1.GetOptions{})
			if err != nil {
				writeError(w, http.StatusInternalServerError, "get existing SecretReference failed: "+err.Error())
				return
			}
		} else {
			slog.Error("SecretReference create failed, cleaning up vault", "name", secretRefName, "error", err)
			if delErr := s.vaultDelete(r.Context(), vaultPath); delErr != nil {
				slog.Error("vault cleanup failed", "path", vaultPath, "error", delErr)
			}
			writeError(w, http.StatusInternalServerError, "create SecretReference failed: "+err.Error())
			return
		}
	}

	var resp secretResponse
	resp.Metadata.ID = secretRefName
	resp.Metadata.Namespace = ns
	resp.Metadata.Labels = req.Metadata.Labels
	resp.Metadata.CreationTimestamp = created.GetCreationTimestamp().Time
	resp.Spec.Keys = keys
	resp.Spec.SecretReferenceName = secretRefName
	writeJSON(w, http.StatusCreated, resp)
}

func (s *server) handleList(w http.ResponseWriter, r *http.Request) {
	ns, ok := s.namespaceFromRequest(w, r)
	if !ok {
		return
	}
	// Always fence by managed-by; merge the caller's selector (the BFF's
	// resolveSecretID passes org/entity labels here).
	selector := managedByLabel + "=" + managedByValue
	if extra := strings.TrimSpace(r.URL.Query().Get("labelSelector")); extra != "" {
		selector += "," + extra
	}
	list, err := s.dyn.Resource(secretReferenceGVR).Namespace(ns).List(r.Context(), metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		slog.Error("list SecretReferences failed", "ns", ns, "selector", selector, "error", err)
		writeError(w, http.StatusInternalServerError, "list failed: "+err.Error())
		return
	}
	out := listResponse{Items: make([]secretResponse, 0, len(list.Items))}
	for i := range list.Items {
		out.Items = append(out.Items, crToResponse(&list.Items[i]))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *server) handleDelete(w http.ResponseWriter, r *http.Request) {
	ns, ok := s.namespaceFromRequest(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing secret id")
		return
	}
	vaultPath := fmt.Sprintf("%s/%s/%s", s.cfg.vaultPathPrefix, ns, id)
	if err := s.vaultDelete(r.Context(), vaultPath); err != nil {
		slog.Error("vault delete failed", "path", vaultPath, "error", err)
		writeError(w, http.StatusInternalServerError, "vault delete failed: "+err.Error())
		return
	}
	if err := s.dyn.Resource(secretReferenceGVR).Namespace(ns).Delete(r.Context(), id, metav1.DeleteOptions{}); err != nil {
		if !apierrors.IsNotFound(err) {
			slog.Error("delete SecretReference failed", "name", id, "error", err)
			writeError(w, http.StatusInternalServerError, "delete SecretReference failed: "+err.Error())
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// ───────────────────────────────── helpers ─────────────────────────────────

// namespaceFromRequest derives the org base namespace from the JWT `ouId`
// claim — identical to the BFF's OrgBaseNamespace and the real
// auth.GenerateNamespaceName. Writes a 401 and returns ok=false on failure.
func (s *server) namespaceFromRequest(w http.ResponseWriter, r *http.Request) (string, bool) {
	ouID, err := ouIDFromAuthHeader(r.Header.Get("Authorization"))
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return "", false
	}
	return generateNamespaceName(ouID), true
}

func ouIDFromAuthHeader(authHeader string) (string, error) {
	tok := strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
	if tok == "" {
		return "", errors.New("missing bearer token")
	}
	parts := strings.Split(tok, ".")
	if len(parts) < 2 {
		return "", errors.New("malformed JWT")
	}
	payload, err := base64URLDecode(parts[1])
	if err != nil {
		return "", fmt.Errorf("decode JWT payload: %w", err)
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		return "", fmt.Errorf("parse JWT claims: %w", err)
	}
	ouID, _ := claims["ouId"].(string)
	if strings.TrimSpace(ouID) == "" {
		return "", errors.New("'ouId' claim not found in JWT")
	}
	return ouID, nil
}

// base64URLDecode handles JWT segments with or without padding.
func base64URLDecode(seg string) ([]byte, error) {
	seg = strings.TrimRight(seg, "=")
	return base64.RawURLEncoding.DecodeString(seg)
}

// generateNamespaceName mirrors auth.GenerateNamespaceName /
// OrgBaseNamespace: wc-<first-8-of-cleaned-uuid>-<8-char-sha256-hex>.
func generateNamespaceName(orgUUID string) string {
	clean := strings.ReplaceAll(orgUUID, "-", "")
	prefix := clean
	if len(clean) > 8 {
		prefix = clean[:8]
	}
	h := sha256.Sum256([]byte(orgUUID))
	salt := hex.EncodeToString(h[:])[:8]
	return fmt.Sprintf("wc-%s-%s", strings.ToLower(prefix), salt)
}

// generateSecretRefName mirrors service.generateSecretRefName:
// {prefix}{sanitized-name}-{12-hex}.
func (s *server) generateSecretRefName(name string) string {
	sanitized := sanitizeDNSName(name)
	if len(sanitized) > maxNamePartLength {
		sanitized = sanitized[:maxNamePartLength]
	}
	sanitized = strings.TrimRight(sanitized, "-")
	return fmt.Sprintf("%s%s-%s", s.cfg.secretRefPrefix, sanitized, randomHex12())
}

// sanitizeDNSName mirrors service.sanitizeDNSName.
func sanitizeDNSName(name string) string {
	name = strings.ToLower(name)
	var b strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteRune('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

// buildSecretReference mirrors service.buildSecretReference.
func (s *server) buildSecretReference(name, namespace, vaultPath string, keys []string, userLabels map[string]string) *unstructured.Unstructured {
	dataEntries := make([]any, 0, len(keys))
	for _, key := range keys {
		dataEntries = append(dataEntries, map[string]any{
			"secretKey": key,
			"remoteRef": map[string]any{
				"key":      vaultPath,
				"property": key,
			},
		})
	}
	labels := map[string]any{managedByLabel: managedByValue}
	for k, v := range userLabels {
		labels[k] = v
	}
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": secretRefAPIVersion,
		"kind":       secretRefKind,
		"metadata": map[string]any{
			"name":      name,
			"namespace": namespace,
			"labels":    labels,
		},
		"spec": map[string]any{
			"refreshInterval": s.cfg.refreshInterval,
			"template":        map[string]any{"type": "Opaque"},
			"data":            dataEntries,
		},
	}}
}

// crToResponse extracts the response shape from a SecretReference CR.
// metadata.id == the CR name == secretReferenceName (the BFF's Delete
// path reads items[0].metadata.id and feeds it back as DELETE /secrets/{id}).
func crToResponse(obj *unstructured.Unstructured) secretResponse {
	var resp secretResponse
	name := obj.GetName()
	resp.Metadata.ID = name
	resp.Metadata.Namespace = obj.GetNamespace()
	resp.Metadata.CreationTimestamp = obj.GetCreationTimestamp().Time
	// User labels = all labels minus the reserved-prefix system labels.
	if labels := obj.GetLabels(); len(labels) > 0 {
		user := map[string]string{}
		for k, v := range labels {
			if !strings.HasPrefix(k, "cloud.wso2.com/") {
				user[k] = v
			}
		}
		if len(user) > 0 {
			resp.Metadata.Labels = user
		}
	}
	if data, found, _ := unstructured.NestedSlice(obj.Object, "spec", "data"); found {
		for _, e := range data {
			if m, ok := e.(map[string]any); ok {
				if sk, ok := m["secretKey"].(string); ok {
					resp.Spec.Keys = append(resp.Spec.Keys, sk)
				}
			}
		}
	}
	resp.Spec.SecretReferenceName = name
	return resp
}

// ───────────────────────────────── OpenBao ─────────────────────────────────

// vaultWrite stores data at KV-v2 path `secret/data/<path>` so the result
// matches the real ESO vault push (each map key = one field/property).
func (s *server) vaultWrite(ctx context.Context, path string, data map[string]string) error {
	body, err := json.Marshal(map[string]any{"data": data})
	if err != nil {
		return err
	}
	url := s.cfg.openbaoAddr + "/v1/secret/data/" + path
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("X-Vault-Token", s.cfg.openbaoToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("openbao returned %d: %s", resp.StatusCode, string(b))
	}
	return nil
}

// vaultDelete removes all versions + metadata (KV-v2 metadata delete).
// A 404 is tolerated by the caller path; here it is treated as success.
func (s *server) vaultDelete(ctx context.Context, path string) error {
	url := s.cfg.openbaoAddr + "/v1/secret/metadata/" + path
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Vault-Token", s.cfg.openbaoToken)
	resp, err := s.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusNotFound {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("openbao returned %d: %s", resp.StatusCode, string(b))
	}
	return nil
}

// ───────────────────────────────── plumbing ─────────────────────────────────

func loadKubeRESTConfig() (*rest.Config, error) {
	if cfg, err := rest.InClusterConfig(); err == nil {
		return cfg, nil
	}
	loader := clientcmd.NewDefaultClientConfigLoadingRules()
	cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loader, &clientcmd.ConfigOverrides{}).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("load kubeconfig: %w", err)
	}
	return cfg, nil
}

func withRequestLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		slog.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"durationMs", time.Since(start).Milliseconds())
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError matches the real service's {"error":...} shape (the BFF
// provider's parseError reads `error`/`message`).
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func randomHex12() string {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		// rand.Read never fails on supported platforms; fall back to time.
		return hex.EncodeToString([]byte(fmt.Sprintf("%012d", time.Now().UnixNano()))[:6])
	}
	return hex.EncodeToString(b)
}

func envOr(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func parseLogLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
