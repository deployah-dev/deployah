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

package k8s

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"

	"deployah.dev/deployah/internal/spec"
)

// SelectorBuilder helps build Kubernetes label selectors for Deployah resources
type SelectorBuilder struct {
	selector labels.Selector
}

// NewSelectorBuilder constructs an empty [SelectorBuilder].
func NewSelectorBuilder() *SelectorBuilder {
	return &SelectorBuilder{
		selector: labels.NewSelector(),
	}
}

// WithProject adds a project label requirement to the selector
func (sb *SelectorBuilder) WithProject(project string) (*SelectorBuilder, error) {
	if project == "" {
		return sb, nil
	}

	req, err := labels.NewRequirement(ProjectLabel, selection.Equals, []string{project})
	if err != nil {
		return nil, fmt.Errorf("failed to create project label requirement: %w", err)
	}
	sb.selector = sb.selector.Add(*req)
	return sb, nil
}

// WithComponent adds a component label requirement to the selector
func (sb *SelectorBuilder) WithComponent(component string) (*SelectorBuilder, error) {
	if component == "" {
		return sb, nil
	}

	req, err := labels.NewRequirement(ComponentLabel, selection.Equals, []string{component})
	if err != nil {
		return nil, fmt.Errorf("failed to create component label requirement: %w", err)
	}
	sb.selector = sb.selector.Add(*req)
	return sb, nil
}

// WithEnvironment adds an environment label requirement to the selector.
// The value is the logical environment ([spec.EnvIdentity.MapKey]), so
// review/pr-42 matches deployah.dev/environment=review.
func (sb *SelectorBuilder) WithEnvironment(environment string) (*SelectorBuilder, error) {
	if environment == "" {
		return sb, nil
	}

	req, err := labels.NewRequirement(EnvironmentLabel, selection.Equals, []string{spec.NormalizeEnv(environment).MapKey})
	if err != nil {
		return nil, fmt.Errorf("failed to create environment label requirement: %w", err)
	}
	sb.selector = sb.selector.Add(*req)
	return sb, nil
}

// WithInstance adds an app.kubernetes.io/instance requirement. Use this
// when the operation must target one Helm release, not every resource
// that shares the logical environment label.
func (sb *SelectorBuilder) WithInstance(instance string) (*SelectorBuilder, error) {
	if instance == "" {
		return sb, nil
	}

	req, err := labels.NewRequirement(InstanceLabel, selection.Equals, []string{instance})
	if err != nil {
		return nil, fmt.Errorf("failed to create instance label requirement: %w", err)
	}
	sb.selector = sb.selector.Add(*req)
	return sb, nil
}

// Build returns the final label selector string.
func (sb *SelectorBuilder) Build() string {
	return sb.selector.String()
}

// BuildSelector builds a label selector from project, component, and environment.
func BuildSelector(project, component, environment string) (string, error) {
	builder := NewSelectorBuilder()

	var err error
	builder, err = builder.WithProject(project)
	if err != nil {
		return "", err
	}

	builder, err = builder.WithComponent(component)
	if err != nil {
		return "", err
	}

	builder, err = builder.WithEnvironment(environment)
	if err != nil {
		return "", err
	}

	if instance := wildcardReleaseInstance(project, environment); instance != "" {
		builder, err = builder.WithInstance(instance)
		if err != nil {
			return "", err
		}
	}

	return builder.Build(), nil
}

// BuildProjectSelector builds a selector for a specific project
func BuildProjectSelector(project string) (string, error) {
	return BuildSelector(project, "", "")
}

// BuildComponentSelector builds a selector for a specific project and component
func BuildComponentSelector(project, component string) (string, error) {
	return BuildSelector(project, component, "")
}

// BuildLabelSelector returns a labels.Selector for project and/or
// environment filters.
func BuildLabelSelector(project, environment string) (labels.Selector, error) {
	selector := labels.NewSelector()
	if project != "" {
		req, err := labels.NewRequirement(ProjectLabel, selection.Equals, []string{project})
		if err != nil {
			return nil, fmt.Errorf("project label: %w", err)
		}
		selector = selector.Add(*req)
	}
	if environment != "" {
		req, err := labels.NewRequirement(EnvironmentLabel, selection.Equals, []string{spec.NormalizeEnv(environment).MapKey})
		if err != nil {
			return nil, fmt.Errorf("environment label: %w", err)
		}
		selector = selector.Add(*req)
	}
	return selector, nil
}

// releaseInstance is the Helm release name for project and environment.
// Empty when either argument is empty.
func releaseInstance(project, environment string) string {
	if project == "" || environment == "" {
		return ""
	}
	return spec.NormalizeEnv(environment).ReleaseName(project)
}

// withReleaseInstance adds an [InstanceLabel] requirement for the Helm
// release of project and environment. No-op when either is empty.
func withReleaseInstance(selector labels.Selector, project, environment string) (labels.Selector, error) {
	instance := releaseInstance(project, environment)
	if instance == "" {
		return selector, nil
	}
	req, err := labels.NewRequirement(InstanceLabel, selection.Equals, []string{instance})
	if err != nil {
		return nil, fmt.Errorf("instance label: %w", err)
	}
	return selector.Add(*req), nil
}

// wildcardReleaseInstance is the Helm release name when environment is a
// wildcard instance such as review/pr-123. Empty when the name is already
// the logical environment, so pod selectors stay logical-only.
func wildcardReleaseInstance(project, environment string) string {
	if project == "" || environment == "" {
		return ""
	}
	env := spec.NormalizeEnv(environment)
	if env.Original == env.MapKey {
		return ""
	}
	return env.ReleaseName(project)
}

// environmentFromLabels is the environment name callers should pass to
// [BuildSelector] for this pod. Logical releases stay as MapKey; wildcard
// instances reconstruct review/pr-123 from the Helm instance label.
func environmentFromLabels(project string, podLabels map[string]string) string {
	mapKey := podLabels[EnvironmentLabel]
	if mapKey == "" {
		return ""
	}
	instance := podLabels[InstanceLabel]
	if instance == "" || project == "" {
		return mapKey
	}
	logicalRelease := spec.NormalizeEnv(mapKey).ReleaseName(project)
	if instance == logicalRelease {
		return mapKey
	}
	rest, ok := strings.CutPrefix(instance, logicalRelease+"-")
	if !ok || rest == "" {
		return mapKey
	}
	candidate := mapKey + "/" + rest
	if spec.NormalizeEnv(candidate).ReleaseName(project) == instance {
		return candidate
	}
	return mapKey
}
