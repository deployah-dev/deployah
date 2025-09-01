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

// Package k8s provides functions to interact with Kubernetes selectors.
package k8s

import (
	"fmt"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
)

// SelectorBuilder helps build Kubernetes label selectors for Deployah resources
type SelectorBuilder struct {
	selector labels.Selector
}

// NewSelectorBuilder creates a new selector builder
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

// WithEnvironment adds an environment label requirement to the selector
func (sb *SelectorBuilder) WithEnvironment(environment string) (*SelectorBuilder, error) {
	if environment == "" {
		return sb, nil
	}

	req, err := labels.NewRequirement(EnvironmentLabel, selection.Equals, []string{environment})
	if err != nil {
		return nil, fmt.Errorf("failed to create environment label requirement: %w", err)
	}
	sb.selector = sb.selector.Add(*req)
	return sb, nil
}

// Build returns the final label selector string
func (sb *SelectorBuilder) Build() string {
	return sb.selector.String()
}

// BuildSelector is a convenience function to build a selector with multiple criteria
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
