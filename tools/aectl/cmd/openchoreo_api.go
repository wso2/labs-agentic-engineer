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
	"strconv"
	"time"

	"github.com/spf13/viper"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/wso2/aep/aectl/internal/envidp"
	k8s "github.com/wso2/aep/aectl/internal/kubernetes"
	"github.com/wso2/aep/aectl/internal/openchoreo"
)

// ocAPIServiceName is the Service OpenChoreo's control-plane chart gives its
// API (the default of openchoreoApi.name). aectl port-forwards to it in
// oc.system_namespace, as it does to Thunder's and OpenBao's fixed names.
const ocAPIServiceName = "openchoreo-api"

// ocAPIPort is the port of OpenChoreo's API Service to forward to: the one
// the chart names http. Its number is a chart value
// (openchoreoApi.service.port), so it is read from the Service.
func ocAPIPort(ctx context.Context, client kubernetes.Interface, namespace string) (string, error) {
	svc, err := client.CoreV1().Services(namespace).Get(ctx, ocAPIServiceName, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("read OpenChoreo's API Service %s/%s (oc.system_namespace): %w", namespace, ocAPIServiceName, err)
	}
	for _, p := range svc.Spec.Ports {
		if p.Name == "http" {
			return strconv.Itoa(int(p.Port)), nil
		}
	}
	return "", fmt.Errorf("OpenChoreo's API Service %s/%s has no port named http", namespace, ocAPIServiceName)
}

func aectlClientSecret(ctx context.Context, client kubernetes.Interface, platformNamespace string) (string, error) {
	sec, err := client.CoreV1().Secrets(platformNamespace).Get(ctx, aectlClientSecretsName, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("read %s/%s, the secret of %s: %w", platformNamespace, aectlClientSecretsName, aectlOpenChoreoClientID, err)
	}
	v := sec.Data["AECTL_OPENCHOREO_CLIENT_SECRET"]
	if len(v) == 0 {
		return "", fmt.Errorf("%s/%s has no AECTL_OPENCHOREO_CLIENT_SECRET; its ESO sync may be incomplete", platformNamespace, aectlClientSecretsName)
	}
	return string(v), nil
}

func secretTargetPlane() openchoreo.TargetPlane {
	p := openchoreo.TargetPlane{
		Kind: viper.GetString("oc.secret_target_plane.kind"),
		Name: viper.GetString("oc.secret_target_plane.name"),
	}
	if p.Kind == "" {
		p.Kind = "ClusterDataPlane"
	}
	if p.Name == "" {
		p.Name = "default"
	}
	return p
}

const ocAPIReachableTimeout = time.Minute

// openChoreoBindingSecrets stores secrets through OpenChoreo's Secret API as
// aectl-openchoreo-client.
type openChoreoBindingSecrets struct {
	client            kubernetes.Interface
	kubeconfig        string
	platformNamespace string
	ocNamespace       string
	tokenURL          string
	plane             openchoreo.TargetPlane
}

var _ envidp.BindingSecrets = openChoreoBindingSecrets{}

func (s openChoreoBindingSecrets) PutSecret(ctx context.Context, namespace, name string, data map[string]string) error {
	clientSecret, err := aectlClientSecret(ctx, s.client, s.platformNamespace)
	if err != nil {
		return err
	}
	port, err := ocAPIPort(ctx, s.client, s.ocNamespace)
	if err != nil {
		return err
	}
	pf, err := k8s.PortForwardService(ctx, s.ocNamespace, ocAPIServiceName, port, s.kubeconfig)
	if err != nil {
		return err
	}
	defer pf.Stop()
	base := "http://localhost:" + pf.Port
	if err := k8s.WaitForReachable(ctx, base+"/", ocAPIReachableTimeout, pf); err != nil {
		return err
	}
	api := &openchoreo.Client{
		BaseURL: base,
		Token: openchoreo.ClientCredentials{
			TokenURL:     s.tokenURL,
			ClientID:     aectlOpenChoreoClientID,
			ClientSecret: clientSecret,
		},
	}
	return api.PutSecret(ctx, namespace, name, s.plane, data)
}
