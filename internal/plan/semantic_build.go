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
		crds []extras.RawFile,
	) (*render.RenderResult, helm.ReleasePrep, func(), error)
}

var _ SemanticBuildClient = (*helm.Client)(nil)

// SemanticBuildInput is the caller-supplied input to [BuildSemanticPlan].
// CRDs must already be loaded; this struct does not read files.
type SemanticBuildInput struct {
	// ClusterContext is the kubeconfig context name stored on the plan header.
	ClusterContext string
	// Resolved is the environment-resolved spec. It must be non-nil with a spec.
	Resolved *spec.ResolvedSpec
	// PostRenderer, when non-nil, is forwarded once to the render client.
	PostRenderer postrenderer.PostRenderer
	// CRDs are already-loaded source files from .deployah/crds/.
	// They are forwarded to Helm chart materialization only.
	CRDs []extras.RawFile
	// CRDDocs are presentation identity for those files. They are not
	// Helm transport and are not parsed here.
	CRDDocs []extras.CRDDoc
	// SkipCRDs stamps ChartCRD lifecycle as skip on a fresh install. It is
	// ignored on upgrade. It does not set Helm Install.SkipCRDs.
	SkipCRDs bool
}

// BuildSemanticPlan renders [spec.ResolvedSpec], predicts Live to
// Predicted resource changes, and returns a [semantic.Plan]. It uses
// the [helm.ReleasePrep] from [SemanticBuildClient.RenderManifestsWithPrep]
// and does not look up Helm history itself. PostRenderer, when non-nil,
// is forwarded once to the render client.
//
// The install target Namespace is predicted on cluster first. Helm
// [predict.Predict] then runs against a plan-local wrapper so a
// same-deploy missing target namespace is not fatal. Chart CRD files
// are forwarded to Helm only; they are not parsed into resource changes.
// Presentation identity comes from [SemanticBuildInput.CRDDocs].
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
	if cluster == nil {
		return semantic.Plan{}, nil, cleanup, fmt.Errorf("semantic plan requires a cluster")
	}

	result, prep, renderCleanup, err := client.RenderManifestsWithPrep(ctx, input.Resolved, input.PostRenderer, input.CRDs)
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

	nsChange, missingNS, err := predictNamespace(ctx, cluster, prep.Operation, result.Namespace)
	if err != nil {
		return semantic.Plan{}, nil, cleanup, err
	}

	wrapper := newPrereqCluster(cluster, missingNS, result.Namespace)
	results, err := predict.Predict(ctx, wrapper, predInput)
	if err != nil {
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
	var changes []semantic.ResourceChange
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
	p, err = semantic.AttachChartCRDs(p, chartCRDsFromDocs(input.CRDDocs, prep.Operation, input.SkipCRDs))
	if err != nil {
		return semantic.Plan{}, nil, cleanup, fmt.Errorf("assemble semantic plan: %w", err)
	}
	return p, result, cleanup, nil
}

func chartCRDsFromDocs(docs []extras.CRDDoc, op helm.Operation, skipCRDs bool) []semantic.ChartCRD {
	lifecycle := semantic.ChartCRDProcess
	willProcess := true
	switch {
	case op == helm.OperationUpgrade:
		lifecycle = semantic.ChartCRDUpgrade
		willProcess = false
	case skipCRDs:
		lifecycle = semantic.ChartCRDSkip
		willProcess = false
	}
	out := make([]semantic.ChartCRD, 0, len(docs))
	for _, d := range docs {
		out = append(out, semantic.ChartCRD{
			Source:      extras.CRDDisplayPath(d.Path),
			Index:       d.Index,
			Kind:        d.Kind,
			Name:        d.Name,
			Lifecycle:   lifecycle,
			WillProcess: willProcess,
		})
	}
	return out
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
