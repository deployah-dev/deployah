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
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"deployah.dev/deployah/internal/extras"
	"deployah.dev/deployah/internal/helm"
	"deployah.dev/deployah/internal/plan/semantic"
	"deployah.dev/deployah/internal/render"
	"deployah.dev/deployah/internal/spec"
)

// RESTMapper maps a GroupKind and version to a REST mapping.
// [BuildSemanticPlan] uses it only to learn scope and whether a
// resource can be built. It does not read live objects or send writes.
// Live reads for Drift go through [LiveReader]. [meta.RESTMapper]
// satisfies it.
type RESTMapper interface {
	RESTMapping(gk schema.GroupKind, versions ...string) (*meta.RESTMapping, error)
}

var _ RESTMapper = meta.RESTMapper(nil)

// SemanticBuildClient is the render [BuildSemanticPlan] calls. It
// returns the render and its prep. A Helm client reads release history
// there and returns Previous on the prep.
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

// BuildSemanticPlan builds a read-only semantic deployment plan from
// the rendered release and, on upgrade, from live cluster state. An
// upgrade requires live. A fresh install may pass a nil reader.
//
// Call the returned cleanup once. It is never nil. On error the plan
// is empty and the render result is nil.
func BuildSemanticPlan(
	ctx context.Context,
	client SemanticBuildClient,
	mapper RESTMapper,
	live LiveReader,
	input SemanticBuildInput,
) (semantic.Plan, *render.RenderResult, func(), error) {
	cleanup := func() {}
	if client == nil {
		return semantic.Plan{}, nil, cleanup, fmt.Errorf("semantic plan requires a helm client")
	}
	if input.Resolved == nil || input.Resolved.Spec == nil {
		return semantic.Plan{}, nil, cleanup, fmt.Errorf("semantic plan requires resolved spec; call spec.Resolve first")
	}
	if mapper == nil {
		return semantic.Plan{}, nil, cleanup, fmt.Errorf("semantic plan requires a REST mapper")
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
	if prep.Operation == helm.OperationUpgrade && live == nil {
		return semantic.Plan{}, nil, cleanup, fmt.Errorf("semantic plan for an existing release requires a live reader")
	}

	helmAction, err := deriveHelmAction(prep, result.Manifest, result.Hooks)
	if err != nil {
		return semantic.Plan{}, nil, cleanup, fmt.Errorf("determine helm release intent: %w", err)
	}

	changes, previous, err := declaredChanges(mapper, prep, result)
	if err != nil {
		return semantic.Plan{}, nil, cleanup, err
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
	tasks, err := assembleTasks(input.Resolved, prep, result.Hooks, changes)
	if err != nil {
		return semantic.Plan{}, nil, cleanup, fmt.Errorf("assemble semantic plan: %w", err)
	}
	drift, err := observeDrift(ctx, live, previous, header)
	if err != nil {
		return semantic.Plan{}, nil, cleanup, fmt.Errorf("observe drift: %w", err)
	}
	p, err := semantic.New(semantic.Input{
		Header:     header,
		HelmAction: helmAction,
		Changes:    changes,
		Tasks:      tasks,
		Drift:      drift,
		ChartCRDs:  chartCRDsFromDocs(input.CRDDocs, prep.Operation, input.SkipCRDs),
	})
	if err != nil {
		return semantic.Plan{}, nil, cleanup, fmt.Errorf("assemble semantic plan: %w", err)
	}
	return p, result, cleanup, nil
}

func declaredChanges(mapper RESTMapper, prep helm.ReleasePrep, result *render.RenderResult) ([]semantic.ResourceChange, []declaration, error) {
	desiredObjs, err := flattenManifest(result.Manifest)
	if err != nil {
		return nil, nil, fmt.Errorf("rendered manifest: %w", err)
	}
	if ownErr := checkTargetNamespaceOwnership("rendered manifest", desiredObjs, result.Namespace); ownErr != nil {
		return nil, nil, ownErr
	}
	desired, err := declareAll(mapper, desiredObjs, result.Namespace, "rendered manifest")
	if err != nil {
		return nil, nil, err
	}

	var previous []declaration
	if prep.Operation == helm.OperationUpgrade {
		if prep.Current == nil {
			return nil, nil, fmt.Errorf("upgrade requires the previous release")
		}
		side := fmt.Sprintf("previous release revision %d", prep.Current.Version)
		previousObjs, flatErr := flattenManifest(prep.Current.Manifest)
		if flatErr != nil {
			return nil, nil, fmt.Errorf("%s: %w", side, flatErr)
		}
		if ownErr := checkTargetNamespaceOwnership(side, previousObjs, result.Namespace); ownErr != nil {
			return nil, nil, ownErr
		}
		previous, err = declareAll(mapper, previousObjs, result.Namespace, side)
		if err != nil {
			return nil, nil, err
		}
	}

	changes, err := diffDeclared(previous, desired)
	if err != nil {
		return nil, nil, err
	}
	if err = stampHelmOrder(changes); err != nil {
		return nil, nil, fmt.Errorf("assemble semantic plan: %w", err)
	}
	return changes, previous, nil
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
