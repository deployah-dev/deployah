// Copyright 2025 The Deployah Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package localkube

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"gopherly.dev/currus"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// registryAuthSecretName is the fixed name of the Kubernetes Secret written by
// SyncRegistryAuth. Using a predictable name keeps the operation idempotent and
// makes it easy for users to reference the secret in their Helm values or
// custom manifests.
const registryAuthSecretName = "deployah-registry-auth"

// dockerConfigJSON is the in-memory representation of a Kubernetes
// dockerconfigjson secret payload.
//
//nolint:tagliatelle // auths key is mandated by the Kubernetes wire format
type dockerConfigJSON struct {
	Auths map[string]dockerConfigAuth `json:"auths"`
}

// dockerConfigAuth holds the per-registry credentials inside a
// dockerconfigjson blob.
//
//nolint:tagliatelle // field names are mandated by the Kubernetes/Docker wire format
type dockerConfigAuth struct {
	Username      string `json:"username,omitempty"`
	Password      string `json:"password,omitempty"`
	Auth          string `json:"auth,omitempty"`
	IdentityToken string `json:"identitytoken,omitempty"`
	RegistryToken string `json:"registrytoken,omitempty"`
}

// syncRegistryAuth resolves host registry credentials through the
// CredentialProvider capability of the given engine, then creates or updates a
// Kubernetes dockerconfigjson Secret named [registryAuthSecretName] in the
// target namespace of the cluster identified by kubeConfigBytes.
//
// After writing the Secret it also patches the "default" ServiceAccount in the
// same namespace to include the Secret in its imagePullSecrets, so that all
// pods in the namespace can pull private images without any additional
// Helm/manifest changes.
//
// Non-fatal outcomes are logged at debug level and return nil:
//   - engine is nil (no Docker/Podman available)
//   - engine does not implement CredentialProvider
//   - Credentials() returns an empty map
//
// An error is returned only for hard failures (K8s API errors, kubeconfig
// parse errors, credential resolution errors).
func syncRegistryAuth(
	ctx context.Context,
	logger *slog.Logger,
	eng currus.Engine,
	kubeConfigBytes []byte,
	namespace string,
) error {
	if eng == nil {
		logger.Debug("localkube: no container engine; skipping registry auth sync")
		return nil
	}

	cp, ok := eng.(currus.CredentialProvider)
	if !ok {
		logger.Debug("localkube: engine does not support CredentialProvider; skipping registry auth sync",
			"kind", eng.Kind())
		return nil
	}

	creds, err := cp.Credentials(ctx)
	if err != nil {
		return fmt.Errorf("localkube: resolve registry credentials: %w", err)
	}
	if len(creds) == 0 {
		logger.Debug("localkube: no registry credentials found; skipping registry auth sync")
		return nil
	}

	secretData, err := buildDockerConfigJSON(creds)
	if err != nil {
		return fmt.Errorf("localkube: build dockerconfigjson: %w", err)
	}

	client, err := k8sClientFromKubeConfig(kubeConfigBytes)
	if err != nil {
		return fmt.Errorf("localkube: build k8s client for registry auth sync: %w", err)
	}

	if applyErr := applyRegistryAuthSecret(ctx, client, namespace, secretData); applyErr != nil {
		return fmt.Errorf("localkube: apply registry auth secret: %w", applyErr)
	}

	if patchErr := patchDefaultServiceAccount(ctx, client, namespace); patchErr != nil {
		return fmt.Errorf("localkube: patch default service account: %w", patchErr)
	}

	logger.Debug("localkube: registry credentials synced",
		"secret", registryAuthSecretName,
		"namespace", namespace,
		"registries", len(creds))
	return nil
}

// buildDockerConfigJSON converts a map of currus.AuthEntry values to the
// raw JSON bytes expected by a Kubernetes dockerconfigjson Secret.
func buildDockerConfigJSON(creds map[string]currus.AuthEntry) ([]byte, error) {
	cfg := dockerConfigJSON{
		Auths: make(map[string]dockerConfigAuth, len(creds)),
	}
	for serverURL, entry := range creds {
		cfg.Auths[serverURL] = dockerConfigAuth{
			Username:      entry.Username,
			Password:      entry.Password,
			Auth:          entry.Auth,
			IdentityToken: entry.IdentityToken,
			RegistryToken: entry.RegistryToken,
		}
	}
	return json.Marshal(cfg)
}

// k8sClientFromKubeConfig builds a Kubernetes clientset from raw kubeconfig
// bytes, following the same pattern used in kind.go for status checks.
func k8sClientFromKubeConfig(kubeConfigBytes []byte) (kubernetes.Interface, error) {
	restCfg, err := clientcmd.RESTConfigFromKubeConfig(kubeConfigBytes)
	if err != nil {
		return nil, fmt.Errorf("parse kubeconfig: %w", err)
	}
	client, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		return nil, fmt.Errorf("build client: %w", err)
	}
	return client, nil
}

// applyRegistryAuthSecret creates the dockerconfigjson Secret in the given
// namespace, or updates it in place when it already exists (idempotent).
func applyRegistryAuthSecret(
	ctx context.Context,
	client kubernetes.Interface,
	namespace string,
	secretData []byte,
) error {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      registryAuthSecretName,
			Namespace: namespace,
		},
		Type: corev1.SecretTypeDockerConfigJson,
		Data: map[string][]byte{
			corev1.DockerConfigJsonKey: secretData,
		},
	}

	_, err := client.CoreV1().Secrets(namespace).Create(ctx, secret, metav1.CreateOptions{})
	if err == nil {
		return nil
	}
	if !k8serrors.IsAlreadyExists(err) {
		return fmt.Errorf("create secret: %w", err)
	}

	// Secret already exists — fetch it, overwrite the data, and update.
	existing, err := client.CoreV1().Secrets(namespace).Get(ctx, registryAuthSecretName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get existing secret: %w", err)
	}
	existing.Type = corev1.SecretTypeDockerConfigJson
	existing.Data = secret.Data
	_, err = client.CoreV1().Secrets(namespace).Update(ctx, existing, metav1.UpdateOptions{})
	return err
}

// patchDefaultServiceAccount ensures the "default" ServiceAccount in the given
// namespace contains [registryAuthSecretName] in its imagePullSecrets, so that
// all pods bound to that SA can pull private images automatically.
// The patch is idempotent: if the entry already exists it is not duplicated.
func patchDefaultServiceAccount(
	ctx context.Context,
	client kubernetes.Interface,
	namespace string,
) error {
	sa, err := client.CoreV1().ServiceAccounts(namespace).Get(ctx, "default", metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get default service account: %w", err)
	}

	// Check whether the secret is already referenced.
	for _, ref := range sa.ImagePullSecrets {
		if ref.Name == registryAuthSecretName {
			return nil // already present, nothing to do
		}
	}

	sa.ImagePullSecrets = append(sa.ImagePullSecrets, corev1.LocalObjectReference{
		Name: registryAuthSecretName,
	})
	_, err = client.CoreV1().ServiceAccounts(namespace).Update(ctx, sa, metav1.UpdateOptions{})
	return err
}
