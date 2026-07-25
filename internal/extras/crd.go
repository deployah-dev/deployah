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

package extras

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	sigsyaml "sigs.k8s.io/yaml"
)

// Policy controls how Deployah installs CRDs from .deployah/crds/.
type Policy string

const (
	// PolicyCreate installs a CRD only when it does not already exist.
	PolicyCreate Policy = "create"
	// PolicyCreateReplace creates a missing CRD or server-side-applies over
	// an existing one (force ownership).
	PolicyCreateReplace Policy = "create-replace"

	crdFieldManager = "deployah"
)

// CRDStats summarizes what [ApplyCRDs] did for one deploy.
type CRDStats struct {
	// Created is how many CRDs were newly installed.
	Created int
	// Replaced is how many existing CRDs were server-side-applied.
	Replaced int
	// Ready is how many CRDs were waited on (created, replaced, or already present).
	Ready int
}

// crdClient is the subset of the apiextensions API used by ApplyCRDs.
type crdClient interface {
	Create(ctx context.Context, crd *apiextensionsv1.CustomResourceDefinition, opts metav1.CreateOptions) (*apiextensionsv1.CustomResourceDefinition, error)
	// Apply server-side-applies patch (JSON, content-type apply-patch+yaml) for name.
	Apply(ctx context.Context, name string, patch []byte) (*apiextensionsv1.CustomResourceDefinition, error)
	Get(ctx context.Context, name string, opts metav1.GetOptions) (*apiextensionsv1.CustomResourceDefinition, error)
}

type crdClientAdapter struct {
	inner apiextensionsclient.Interface
}

func (a crdClientAdapter) Create(ctx context.Context, crd *apiextensionsv1.CustomResourceDefinition, opts metav1.CreateOptions) (*apiextensionsv1.CustomResourceDefinition, error) {
	return a.inner.ApiextensionsV1().CustomResourceDefinitions().Create(ctx, crd, opts)
}

func (a crdClientAdapter) Apply(ctx context.Context, name string, patch []byte) (*apiextensionsv1.CustomResourceDefinition, error) {
	force := true
	return a.inner.ApiextensionsV1().CustomResourceDefinitions().Patch(
		ctx,
		name,
		types.ApplyPatchType,
		patch,
		metav1.PatchOptions{FieldManager: crdFieldManager, Force: &force},
	)
}

func (a crdClientAdapter) Get(ctx context.Context, name string, opts metav1.GetOptions) (*apiextensionsv1.CustomResourceDefinition, error) {
	return a.inner.ApiextensionsV1().CustomResourceDefinitions().Get(ctx, name, opts)
}

// ApplyCRDs installs CRDs according to policy, then waits for each to report
// Established. CRDs are never pruned. timeout bounds the wait.
//
// create-replace uses server-side apply with a patch derived from the object
// YAML (status and server-managed metadata stripped). See
// https://kubernetes.io/docs/reference/using-api/server-side-apply/
func ApplyCRDs(ctx context.Context, cfg *rest.Config, crds []Object, policy Policy, timeout time.Duration) (CRDStats, error) {
	var stats CRDStats
	if len(crds) == 0 {
		return stats, nil
	}
	if cfg == nil {
		return stats, fmt.Errorf("apply CRDs: cluster configuration is required")
	}
	switch policy {
	case PolicyCreate, PolicyCreateReplace:
	default:
		return stats, fmt.Errorf("unknown CRD policy %q", policy)
	}
	cs, err := apiextensionsclient.NewForConfig(cfg)
	if err != nil {
		return stats, fmt.Errorf("build apiextensions client: %w", err)
	}
	return applyCRDs(ctx, crdClientAdapter{inner: cs}, crds, policy, timeout)
}

func applyCRDs(ctx context.Context, client crdClient, crds []Object, policy Policy, timeout time.Duration) (CRDStats, error) {
	var stats CRDStats
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	applied := make([]string, 0, len(crds))
	for i := range crds {
		crd, err := decodeCRD(crds[i])
		if err != nil {
			return stats, err
		}
		existing, getErr := client.Get(ctx, crd.Name, metav1.GetOptions{})
		switch {
		case apierrors.IsNotFound(getErr):
			if policy == PolicyCreateReplace {
				patch, patchErr := ssaPatchFromObject(crds[i])
				if patchErr != nil {
					return stats, patchErr
				}
				if _, applyErr := client.Apply(ctx, crd.Name, patch); applyErr != nil {
					return stats, fmt.Errorf("apply CRD %s: %w", crd.Name, applyErr)
				}
			} else if _, createErr := client.Create(ctx, crd, metav1.CreateOptions{}); createErr != nil {
				return stats, fmt.Errorf("create CRD %s: %w", crd.Name, createErr)
			}
			stats.Created++
			applied = append(applied, crd.Name)
		case getErr != nil:
			return stats, fmt.Errorf("get CRD %s: %w", crd.Name, getErr)
		case policy == PolicyCreate:
			applied = append(applied, existing.Name)
		case policy == PolicyCreateReplace:
			patch, patchErr := ssaPatchFromObject(crds[i])
			if patchErr != nil {
				return stats, patchErr
			}
			if _, applyErr := client.Apply(ctx, crd.Name, patch); applyErr != nil {
				return stats, fmt.Errorf("replace CRD %s: %w", crd.Name, applyErr)
			}
			stats.Replaced++
			applied = append(applied, crd.Name)
		}
	}

	deadline := time.Now().Add(timeout)
	for _, name := range applied {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return stats, fmt.Errorf("timed out waiting for CRD %s to become Established", name)
		}
		if err := waitEstablished(ctx, client, name, remaining); err != nil {
			switch {
			case errors.Is(err, context.DeadlineExceeded):
				return stats, fmt.Errorf("timed out waiting for CRD %s to become Established: %w", name, err)
			case errors.Is(err, context.Canceled):
				return stats, fmt.Errorf("waiting for CRD %s canceled: %w", name, err)
			default:
				return stats, fmt.Errorf("wait for CRD %s to become Established: %w", name, err)
			}
		}
		stats.Ready++
	}
	return stats, nil
}

func decodeCRD(o Object) (*apiextensionsv1.CustomResourceDefinition, error) {
	var crd apiextensionsv1.CustomResourceDefinition
	if err := sigsyaml.Unmarshal(o.Raw, &crd); err != nil {
		return nil, fmt.Errorf("%s: decode CRD: %w", o.Path, err)
	}
	if crd.Name == "" {
		return nil, fmt.Errorf("%s: CRD metadata.name is empty", o.Path)
	}
	return &crd, nil
}

// ssaPatchFromObject builds a server-side apply body from the object's YAML.
// Status and server-managed metadata are stripped so the patch matches the
// intended document rather than a typed round-trip full of null fields.
func ssaPatchFromObject(o Object) ([]byte, error) {
	var obj map[string]any
	if err := sigsyaml.Unmarshal(o.Raw, &obj); err != nil {
		return nil, fmt.Errorf("%s: decode for apply: %w", o.Path, err)
	}
	delete(obj, "status")
	if meta, ok := obj["metadata"].(map[string]any); ok {
		delete(meta, "managedFields")
		delete(meta, "resourceVersion")
		delete(meta, "uid")
		delete(meta, "creationTimestamp")
		delete(meta, "generation")
		delete(meta, "selfLink")
	}
	patch, err := json.Marshal(obj)
	if err != nil {
		return nil, fmt.Errorf("%s: marshal apply patch: %w", o.Path, err)
	}
	return patch, nil
}

func waitEstablished(ctx context.Context, client crdClient, name string, timeout time.Duration) error {
	return wait.PollUntilContextTimeout(ctx, 200*time.Millisecond, timeout, true, func(ctx context.Context) (bool, error) {
		crd, err := client.Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		for _, cond := range crd.Status.Conditions {
			if cond.Type == apiextensionsv1.Established && cond.Status == apiextensionsv1.ConditionTrue {
				return true, nil
			}
		}
		return false, nil
	})
}
