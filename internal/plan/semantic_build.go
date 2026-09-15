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

	"helm.sh/helm/v4/pkg/postrenderer"

	"deployah.dev/deployah/internal/extras"
	"deployah.dev/deployah/internal/helm"
	"deployah.dev/deployah/internal/plan/semantic"
	"deployah.dev/deployah/internal/predict"
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

// BuildSemanticPlan renders [spec.ResolvedSpec], predicts Live to
// Predicted resource changes, and returns a [semantic.Plan]. It uses
// the [helm.ReleasePrep] from [SemanticBuildClient.RenderManifestsWithPrep]
// and does not look up Helm history itself. PostRenderer, when non-nil,
// is forwarded once to the render client.
//
// CRDs and the install target Namespace are predicted on cluster first.
// Helm [predict.Predict] then runs against a plan-local wrapper so
// same-deploy missing APIs and namespaces are not fatal.
//
// The caller must invoke the returned cleanup func once. Cleanup is
// always non-nil. On error the plan is empty and the render result is
// nil.
func BuildSemanticPlan(
	ctx context.Context,
	client SemanticBuildClient,
	cluster predict.Cluster,
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
	err = validateRenderPrep(result, prep)
	if err != nil {
		return semantic.Plan{}, nil, cleanup, fmt.Errorf("render preparation: %w", err)
	}

	predInput, err := predict.InputFromPrep(&prep, result.ReleaseName, result.Namespace, result.Manifest)
	if err != nil {
		return semantic.Plan{}, nil, cleanup, fmt.Errorf("build prediction input: %w", err)
	}

	if prep.Operation == helm.OperationInstall {
		if overlapErr := checkInstallNamespaceOverlap(result.Manifest, result.Namespace); overlapErr != nil {
			return semantic.Plan{}, nil, cleanup, overlapErr
		}
	}

	crdChanges, surface, err := predictCRDs(ctx, cluster, input.CRDs, input.CRDPolicy)
	if err != nil {
		return semantic.Plan{}, nil, cleanup, fmt.Errorf("predict CRDs: %w", err)
	}
	nsChange, missingNS, err := predictNamespace(ctx, cluster, prep.Operation, result.Namespace)
	if err != nil {
		return semantic.Plan{}, nil, cleanup, err
	}
	if apiErr := checkRenderedAPIs([]string{result.Manifest, predInput.Previous}, surface); apiErr != nil {
		return semantic.Plan{}, nil, cleanup, apiErr
	}

	wrapper := newPrereqCluster(cluster, missingNS, result.Namespace, surface)
	results, err := predict.Predict(ctx, wrapper, predInput)
	if err != nil {
		if len(surface.specChanged) > 0 {
			return semantic.Plan{}, nil, cleanup, fmt.Errorf("cannot predict helm resources against the current API: %w; at least one CRD spec changes before helm executes and post-CRD behavior cannot be simulated", err)
		}
		return semantic.Plan{}, nil, cleanup, fmt.Errorf("predict resources: %w", err)
	}

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
	helmChanges, diags, err := mapPredictResults(origin, results)
	if err != nil {
		return semantic.Plan{}, nil, cleanup, fmt.Errorf("assemble semantic plan: %w", err)
	}
	diags = append(diags, limitationDiagnostics(wrapper, results)...)
	if orderErr := stampHelmApplyOrder(helmChanges); orderErr != nil {
		return semantic.Plan{}, nil, cleanup, fmt.Errorf("assemble semantic plan: %w", orderErr)
	}
	changes := append([]semantic.ResourceChange{}, crdChanges...)
	if nsChange != nil {
		changes = append(changes, *nsChange)
	}
	changes = append(changes, helmChanges...)
	tasks, err := assembleTasks(input.Resolved, prep, result.Hooks, helmChanges)
	if err != nil {
		return semantic.Plan{}, nil, cleanup, fmt.Errorf("assemble semantic plan: %w", err)
	}
	helmAction := deriveHelmAction(prep.Operation, helmChanges, tasks)
	applyHelmWillRun(tasks, helmAction)
	p, err := semantic.New(header, helmAction, changes, tasks, diags)
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
