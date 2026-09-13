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

package plan

import (
	"context"
	"fmt"

	"helm.sh/helm/v4/pkg/postrenderer"

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

// BuildSemanticPlan renders [spec.ResolvedSpec], predicts Live to
// Predicted resource changes, and returns a [semantic.Plan]. It uses
// the [helm.ReleasePrep] from [SemanticBuildClient.RenderManifestsWithPrep]
// and does not look up Helm history itself. postRenderer, when non-nil,
// is forwarded once to the render client.
//
// The caller must invoke the returned cleanup func once. Cleanup is
// always non-nil. On error the plan is empty and the render result is
// nil.
func BuildSemanticPlan(
	ctx context.Context,
	client SemanticBuildClient,
	cluster predict.Cluster,
	clusterContext string,
	resolved *spec.ResolvedSpec,
	postRenderer postrenderer.PostRenderer,
) (semantic.Plan, *render.RenderResult, func(), error) {
	cleanup := func() {}
	if client == nil {
		return semantic.Plan{}, nil, cleanup, fmt.Errorf("semantic plan requires a helm client")
	}
	if resolved == nil || resolved.Spec == nil {
		return semantic.Plan{}, nil, cleanup, fmt.Errorf("semantic plan requires resolved spec; call spec.Resolve first")
	}

	result, prep, renderCleanup, err := client.RenderManifestsWithPrep(ctx, resolved, postRenderer)
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

	input, err := predict.InputFromPrep(&prep, result.ReleaseName, result.Namespace, result.Manifest)
	if err != nil {
		return semantic.Plan{}, nil, cleanup, fmt.Errorf("build prediction input: %w", err)
	}

	results, err := predict.Predict(ctx, cluster, input)
	if err != nil {
		return semantic.Plan{}, nil, cleanup, fmt.Errorf("predict resources: %w", err)
	}

	header := semantic.Header{
		Project:      resolved.Spec.Project,
		Environment:  resolved.Env.Original,
		Release:      result.ReleaseName,
		Namespace:    result.Namespace,
		Context:      clusterContext,
		Revision:     prep.NextRevision,
		FreshInstall: prep.Operation == helm.OperationInstall,
	}
	p, err := semanticPlanFromResults(header, results)
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
