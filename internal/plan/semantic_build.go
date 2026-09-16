// Copyright 2026 The Deployah Authors
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

package plan

import (
	"context"
	"fmt"
	"slices"

	"helm.sh/helm/v4/pkg/postrenderer"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"deployah.dev/deployah/internal/extras"
	"deployah.dev/deployah/internal/helm"
	"deployah.dev/deployah/internal/plan/semantic"
	"deployah.dev/deployah/internal/render"
	"deployah.dev/deployah/internal/spec"
)

// SemanticBuildClient is the Helm surface [BuildSemanticPlan] needs:
// one prep-aware client-side render. It does not expose release-history
// lookups. Defined narrowly so this package does not depend on
// [deployah.dev/deployah/internal/session] and tests can inject a
// minimal fake.
type SemanticBuildClient interface {
	RenderManifestsWithPrep(
		ctx context.Context,
		resolved *spec.ResolvedSpec,
		postRenderer postrenderer.PostRenderer,
	) (*render.RenderResult, helm.ReleasePrep, func(), error)
}

var _ SemanticBuildClient = (*helm.Client)(nil)

// SemanticBuildInput is the caller-supplied input to [BuildSemanticPlan].
// CRDs must already be loaded; this struct does not read files.
// CRDPolicy is required even when CRDs is empty.
type SemanticBuildInput struct {
	// ClusterContext is the kubeconfig context name stored on the plan header.
	ClusterContext string
	// Resolved is the environment-resolved spec. It must be non-nil with a spec.
	Resolved *spec.ResolvedSpec
	// PostRenderer, when non-nil, is forwarded once to the render client.
	PostRenderer postrenderer.PostRenderer
	// CRDs are already-loaded CustomResourceDefinition objects, in apply order.
	CRDs []extras.Object
	// CRDPolicy is extras.PolicyCreate or extras.PolicyCreateReplace.
	CRDPolicy extras.Policy
}

// BuildSemanticPlan renders [spec.ResolvedSpec] and returns a
// [semantic.Plan] of Previous/Live/Desired intent. It uses the
// [helm.ReleasePrep] from [SemanticBuildClient.RenderManifestsWithPrep]
// and does not look up Helm history itself. Cluster reads are GET and
// discovery only.
//
// The caller must invoke the returned cleanup func once. Cleanup is
// always non-nil. On error the plan is empty and the render result is
// nil.
func BuildSemanticPlan(
	ctx context.Context,
	client SemanticBuildClient,
	cluster ClusterReader,
	input SemanticBuildInput,
) (semantic.Plan, *render.RenderResult, func(), error) {
	cleanup := func() {}
	if client == nil {
		return semantic.Plan{}, nil, cleanup, fmt.Errorf("semantic plan requires a helm client")
	}
	if input.Resolved == nil || input.Resolved.Spec == nil {
		return semantic.Plan{}, nil, cleanup, fmt.Errorf("semantic plan requires resolved spec; call spec.Resolve first")
	}
	switch input.CRDPolicy {
	case extras.PolicyCreate, extras.PolicyCreateReplace:
	default:
		return semantic.Plan{}, nil, cleanup, fmt.Errorf("unknown CRD policy %q", input.CRDPolicy)
	}
	if cluster == nil {
		return semantic.Plan{}, nil, cleanup, fmt.Errorf("semantic plan requires a cluster")
	}

	result, prep, renderCleanup, err := client.RenderManifestsWithPrep(ctx, input.Resolved, input.PostRenderer)
	if renderCleanup != nil {
		cleanup = renderCleanup
	}
	if err != nil {
		return semantic.Plan{}, nil, cleanup, fmt.Errorf("render manifests: %w", err)
	}
	if prepErr := validateRenderPrep(result, prep); prepErr != nil {
		return semantic.Plan{}, nil, cleanup, fmt.Errorf("render preparation: %w", prepErr)
	}

	previousManifest := ""
	if prep.Current != nil {
		previousManifest = prep.Current.Manifest
	}

	hasChartNS, err := chartContainsTargetNamespace(result.Manifest, result.Namespace)
	if err != nil {
		return semantic.Plan{}, nil, cleanup, err
	}

	crdChanges, surface, err := planCRDs(ctx, cluster, input.CRDs, input.CRDPolicy)
	if err != nil {
		return semantic.Plan{}, nil, cleanup, fmt.Errorf("plan CRDs: %w", err)
	}
	var nsChange semantic.ResourceChange
	var hasNS bool
	if !hasChartNS {
		nsChange, hasNS, err = planNamespace(ctx, cluster, prep.Operation, result.Namespace)
		if err != nil {
			return semantic.Plan{}, nil, cleanup, err
		}
	}
	if apiErr := checkRenderedAPIs([]string{result.Manifest, previousManifest}, surface); apiErr != nil {
		return semantic.Plan{}, nil, cleanup, apiErr
	}

	desired, err := loadPlannedObjects(cluster, surface, result.Manifest, result.Namespace, result.ReleaseName, result.Namespace)
	if err != nil {
		return semantic.Plan{}, nil, cleanup, fmt.Errorf("load desired resources: %w", err)
	}
	previous, err := loadPlannedObjects(cluster, surface, previousManifest, result.Namespace, result.ReleaseName, result.Namespace)
	if err != nil {
		return semantic.Plan{}, nil, cleanup, fmt.Errorf("load previous resources: %w", err)
	}
	pairs := pairHelmResources(previous, desired)

	header := semantic.Header{
		Project:      input.Resolved.Spec.Project,
		Environment:  input.Resolved.Env.Original,
		Release:      result.ReleaseName,
		Namespace:    result.Namespace,
		Context:      input.ClusterContext,
		Revision:     prep.NextRevision,
		FreshInstall: prep.Operation == helm.OperationInstall,
	}
	origin := semantic.ResourceOrigin{
		Kind: semantic.OriginHelm,
		Helm: &semantic.HelmOrigin{
			Release:   header.Release,
			Namespace: header.Namespace,
		},
	}

	hookTasks, err := assembleHookTasks(input.Resolved, prep, result.Hooks)
	if err != nil {
		return semantic.Plan{}, nil, cleanup, fmt.Errorf("assemble semantic plan: %w", err)
	}
	helmAction := deriveHelmAction(prep.Operation, helmResourcesChanged(pairs), hookTasks)

	var helmChanges []semantic.ResourceChange
	live := map[string]*unstructured.Unstructured{}
	if helmAction != semantic.HelmNone || !header.FreshInstall {
		live, err = liveByKey(ctx, cluster, pairs)
		if err != nil {
			return semantic.Plan{}, nil, cleanup, err
		}
	}
	if helmAction != semantic.HelmNone {
		helmChanges, err = helmResourceChanges(origin, pairs, live)
		if err != nil {
			return semantic.Plan{}, nil, cleanup, fmt.Errorf("assemble semantic plan: %w", err)
		}
	}
	drift, err := helmDrift(header.FreshInstall, pairs, live)
	if err != nil {
		return semantic.Plan{}, nil, cleanup, fmt.Errorf("assemble semantic plan: %w", err)
	}

	scheduled, err := assembleScheduledTasks(input.Resolved, prep, helmChanges)
	if err != nil {
		return semantic.Plan{}, nil, cleanup, fmt.Errorf("assemble semantic plan: %w", err)
	}
	tasks := slices.Concat(hookTasks, scheduled)
	applyHelmWillRun(tasks, helmAction)

	changes := append([]semantic.ResourceChange{}, crdChanges...)
	if hasNS {
		changes = append(changes, nsChange)
	}
	changes = append(changes, helmChanges...)
	p, err := semantic.New(header, helmAction, changes, tasks, drift)
	if err != nil {
		return semantic.Plan{}, nil, cleanup, fmt.Errorf("assemble semantic plan: %w", err)
	}
	return p, result, cleanup, nil
}

func validateRenderPrep(result *render.RenderResult, prep helm.ReleasePrep) error {
	if result == nil {
		return fmt.Errorf("render result is required")
	}
	switch prep.Operation {
	case helm.OperationInstall:
		if result.IsUpgrade {
			return fmt.Errorf("prepared install but render used upgrade")
		}
	case helm.OperationUpgrade:
		if prep.Current == nil {
			return fmt.Errorf("upgrade prep requires a current release")
		}
		if !result.IsUpgrade {
			return fmt.Errorf("prepared upgrade but render used install")
		}
	default:
		return fmt.Errorf("invalid helm operation %d", prep.Operation)
	}
	if result.Revision != prep.NextRevision {
		return fmt.Errorf("rendered revision %d does not match prepared revision %d", result.Revision, prep.NextRevision)
	}
	return nil
}
