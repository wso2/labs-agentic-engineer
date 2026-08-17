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

package kubernetes

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/discovery"
	memcache "k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

// Applier applies arbitrary (incl. CRD) manifests to the cluster via
// server-side apply, resolving each object's GVK to a REST resource through
// discovery. It lets aectl manage OpenChoreo/ESO/Gateway CRs without shelling
// out to kubectl.
type Applier struct {
	dyn    dynamic.Interface
	mapper meta.RESTMapper
}

// NewApplier builds an Applier from a kubeconfig (empty => ~/.kube/config).
func NewApplier(kubeconfig string) (*Applier, error) {
	if kubeconfig == "" {
		if home := homedir.HomeDir(); home != "" {
			kubeconfig = filepath.Join(home, ".kube", "config")
		}
	}
	cfg, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return nil, err
	}
	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	dc, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		return nil, err
	}
	mapper := restmapper.NewDeferredDiscoveryRESTMapper(memcache.NewMemCacheClient(dc))
	return &Applier{dyn: dyn, mapper: mapper}, nil
}

// ApplyYAML server-side-applies every document in a (possibly multi-document)
// YAML string. fieldManager identifies the owner for SSA conflict resolution;
// defaultNamespace is used for namespaced objects that omit a namespace.
func (a *Applier) ApplyYAML(ctx context.Context, fieldManager, defaultNamespace, manifests string) error {
	dec := yaml.NewYAMLOrJSONDecoder(bytes.NewReader([]byte(manifests)), 4096)
	for {
		obj := &unstructured.Unstructured{}
		if err := dec.Decode(obj); err != nil {
			if err == io.EOF {
				break
			}
			return fmt.Errorf("decode manifest: %w", err)
		}
		if len(obj.Object) == 0 { // skip empty docs (e.g. trailing "---")
			continue
		}
		if err := a.apply(ctx, fieldManager, defaultNamespace, obj); err != nil {
			return err
		}
	}
	return nil
}

func (a *Applier) apply(ctx context.Context, fieldManager, defaultNamespace string, obj *unstructured.Unstructured) error {
	gvk := obj.GroupVersionKind()
	mapping, err := a.mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", gvk.String(), err)
	}

	var ri dynamic.ResourceInterface
	if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
		ns := obj.GetNamespace()
		if ns == "" {
			ns = defaultNamespace
			obj.SetNamespace(ns)
		}
		ri = a.dyn.Resource(mapping.Resource).Namespace(ns)
	} else {
		ri = a.dyn.Resource(mapping.Resource)
	}

	data, err := json.Marshal(obj.Object)
	if err != nil {
		return fmt.Errorf("marshal %s/%s: %w", obj.GetKind(), obj.GetName(), err)
	}
	force := true
	if _, err := ri.Patch(ctx, obj.GetName(), types.ApplyPatchType, data, metav1.PatchOptions{
		FieldManager: fieldManager,
		Force:        &force,
	}); err != nil {
		return fmt.Errorf("apply %s/%s: %w", obj.GetKind(), obj.GetName(), err)
	}
	return nil
}

// Delete removes a single object identified by apiVersion/kind/namespace/name,
// resolving the GVK through discovery. A missing object is not an error (delete
// is idempotent). namespace is ignored for cluster-scoped kinds.
func (a *Applier) Delete(ctx context.Context, apiVersion, kind, namespace, name string) error {
	gv, err := schema.ParseGroupVersion(apiVersion)
	if err != nil {
		return fmt.Errorf("parse apiVersion %q: %w", apiVersion, err)
	}
	gvk := gv.WithKind(kind)
	mapping, err := a.mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", gvk.String(), err)
	}
	var ri dynamic.ResourceInterface
	if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
		ri = a.dyn.Resource(mapping.Resource).Namespace(namespace)
	} else {
		ri = a.dyn.Resource(mapping.Resource)
	}
	if err := ri.Delete(ctx, name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete %s/%s: %w", kind, name, err)
	}
	return nil
}
