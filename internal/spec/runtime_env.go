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

package spec

import (
	"fmt"
	"maps"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// envKeyRegexp matches POSIX environment variable names.
var envKeyRegexp = regexp.MustCompile(EnvKeyPattern)

// ResolvedRuntimeEnvironment is the resolved runtime environment for one
// component or task. Maps are owned by the resolved model and must be
// treated as read-only after [Resolve] stores them. Downstream code must
// [maps.Clone] before mutating.
type ResolvedRuntimeEnvironment struct {
	// FileValues is the merged dotenv runtime layer.
	FileValues map[string]string
	// ExplicitValues is the merged explicit YAML env: layer.
	ExplicitValues map[string]string
	// EntityValues is the entity-scoped dotenv layer retained only for
	// from: inheritance. It is not a separately delivered runtime stream.
	EntityValues map[string]string
}

// runtimeEnvRequest is the input to [resolveRuntimeEnvironment].
type runtimeEnvRequest struct {
	specDir    string
	envName    string
	env        *Environment
	entityName string
	entityKind string // "component" or "task"
	envFile    string
	explicit   StringMap
	from       string
	parent     *ResolvedRuntimeEnvironment
}

type runtimeKeyHistory struct {
	key     string
	value   string
	sources []string
}

func emptyRuntime() ResolvedRuntimeEnvironment {
	return ResolvedRuntimeEnvironment{
		FileValues:     make(map[string]string),
		ExplicitValues: make(map[string]string),
		EntityValues:   make(map[string]string),
	}
}

func joinSpecPath(specDir, rel string) string {
	if rel == "" {
		return ""
	}
	if filepath.IsAbs(rel) {
		return rel
	}
	return filepath.Join(specDir, rel)
}

func implicitEnvironmentFiles(envName string) []string {
	sanitized := sanitizeEnvName(envName)
	return []string{
		filepath.Join(DeployahConfigDir, DefaultEnvFile),
		DefaultEnvFile,
		filepath.Join(DeployahConfigDir, EnvFilePrefix+sanitized),
		EnvFilePrefix + sanitized,
	}
}

func implicitEntityFiles(entityName, envName string) []string {
	sanitized := sanitizeEnvName(envName)
	return []string{
		filepath.Join(DeployahConfigDir, EnvFilePrefix+entityName),
		EnvFilePrefix + entityName,
		filepath.Join(DeployahConfigDir, EnvFilePrefix+entityName+"."+sanitized),
		EnvFilePrefix + entityName + "." + sanitized,
	}
}

// resolveRuntimeEnvironment discovers dotenv layers and YAML env for one
// entity. It is deterministic and stateless: the same spec, environment,
// specDir, and files produce the same maps. It does not cache or reject a
// second call.
func resolveRuntimeEnvironment(req runtimeEnvRequest) (ResolvedRuntimeEnvironment, []runtimeKeyHistory, []runtimeKeyHistory, error) {
	out := emptyRuntime()

	envVals, envHist, err := loadEnvironmentValues(req.specDir, req.envName, req.env)
	if err != nil {
		return emptyRuntime(), nil, nil, err
	}

	entityVals, entityHist, err := loadEntityValues(req)
	if err != nil {
		return emptyRuntime(), nil, nil, err
	}
	maps.Copy(out.EntityValues, entityVals)

	fileHist := mergeKeyHistories(envHist, entityHist)
	maps.Copy(out.FileValues, envVals)
	maps.Copy(out.FileValues, entityVals)

	explicit := make(map[string]string)
	var explicitHist []runtimeKeyHistory
	if req.parent != nil {
		maps.Copy(explicit, req.parent.ExplicitValues)
		explicitHist = historiesFromMap(req.parent.ExplicitValues, inheritedExplicitSource(req.from))
	}
	if len(req.explicit) > 0 {
		overlay := map[string]string(req.explicit)
		maps.Copy(explicit, overlay)
		explicitHist = mergeKeyHistories(explicitHist, historiesFromMap(overlay, explicitYAMLSource(req.entityKind, req.entityName)))
	}
	maps.Copy(out.ExplicitValues, explicit)

	if fileErr := validateEnvKeys(out.FileValues, "file values"); fileErr != nil {
		return emptyRuntime(), nil, nil, fileErr
	}
	if explicitErr := validateEnvKeys(out.ExplicitValues, "explicit values"); explicitErr != nil {
		return emptyRuntime(), nil, nil, explicitErr
	}

	return out, sortHistories(fileHist, out.FileValues), sortHistories(explicitHist, out.ExplicitValues), nil
}

func loadEnvironmentValues(specDir, envName string, env *Environment) (map[string]string, []runtimeKeyHistory, error) {
	out := make(map[string]string)
	var hist []runtimeKeyHistory
	if env != nil && env.EnvFile != "" {
		path := joinSpecPath(specDir, env.EnvFile)
		vals, err := parseEnvFile(path, true)
		if err != nil {
			return nil, nil, err
		}
		source := fmt.Sprintf("environments.%s.envFile %s", envName, env.EnvFile)
		maps.Copy(out, vals)
		return out, historiesFromMap(vals, source), nil
	}

	for _, rel := range implicitEnvironmentFiles(envName) {
		path := joinSpecPath(specDir, rel)
		vals, err := parseEnvFile(path, false)
		if err != nil {
			return nil, nil, err
		}
		if len(vals) == 0 && !fileExists(path) {
			continue
		}
		maps.Copy(out, vals)
		hist = mergeKeyHistories(hist, historiesFromMap(vals, rel))
	}
	return out, hist, nil
}

func loadEntityValues(req runtimeEnvRequest) (map[string]string, []runtimeKeyHistory, error) {
	out := make(map[string]string)
	if req.envFile != "" {
		path := joinSpecPath(req.specDir, req.envFile)
		vals, err := parseEnvFile(path, true)
		if err != nil {
			return nil, nil, err
		}
		source := fmt.Sprintf("%ss.%s.envFile %s", req.entityKind, req.entityName, req.envFile)
		maps.Copy(out, vals)
		return out, historiesFromMap(vals, source), nil
	}
	if req.parent != nil {
		maps.Copy(out, req.parent.EntityValues)
		source := inheritedEntitySource(req.from)
		return out, historiesFromMap(out, source), nil
	}

	var hist []runtimeKeyHistory
	for _, rel := range implicitEntityFiles(req.entityName, req.envName) {
		path := joinSpecPath(req.specDir, rel)
		vals, err := parseEnvFile(path, false)
		if err != nil {
			return nil, nil, err
		}
		if len(vals) == 0 && !fileExists(path) {
			continue
		}
		maps.Copy(out, vals)
		hist = mergeKeyHistories(hist, historiesFromMap(vals, rel))
	}
	return out, hist, nil
}

func inheritedEntitySource(from string) string {
	if from == "" {
		return "inherited entity values"
	}
	return "inherited entity values from " + from
}

func inheritedExplicitSource(from string) string {
	if from == "" {
		return "inherited explicit values"
	}
	return "inherited explicit values from " + from
}

func explicitYAMLSource(kind, name string) string {
	return fmt.Sprintf("%ss.%s.env", kind, name)
}

func historiesFromMap(vals map[string]string, source string) []runtimeKeyHistory {
	if len(vals) == 0 {
		return nil
	}
	out := make([]runtimeKeyHistory, 0, len(vals))
	for _, k := range slices.Sorted(maps.Keys(vals)) {
		out = append(out, runtimeKeyHistory{key: k, value: vals[k], sources: []string{source}})
	}
	return out
}

func mergeKeyHistories(base, overlay []runtimeKeyHistory) []runtimeKeyHistory {
	byKey := make(map[string]runtimeKeyHistory, len(base)+len(overlay))
	for _, h := range base {
		byKey[h.key] = h
	}
	for _, h := range overlay {
		if existing, ok := byKey[h.key]; ok {
			existing.sources = append(slices.Clone(existing.sources), h.sources...)
			existing.value = h.value
			byKey[h.key] = existing
			continue
		}
		byKey[h.key] = runtimeKeyHistory{
			key:     h.key,
			value:   h.value,
			sources: slices.Clone(h.sources),
		}
	}
	return slices.Collect(maps.Values(byKey))
}

func sortHistories(hist []runtimeKeyHistory, final map[string]string) []runtimeKeyHistory {
	byKey := make(map[string]runtimeKeyHistory, len(hist))
	for _, h := range hist {
		if _, ok := final[h.key]; !ok {
			continue
		}
		h.value = final[h.key]
		byKey[h.key] = h
	}
	out := make([]runtimeKeyHistory, 0, len(final))
	for _, k := range slices.Sorted(maps.Keys(final)) {
		if h, ok := byKey[k]; ok {
			out = append(out, h)
			continue
		}
		out = append(out, runtimeKeyHistory{key: k, value: final[k]})
	}
	return out
}

func validateEnvKeys(vals map[string]string, origin string) error {
	for _, k := range slices.Sorted(maps.Keys(vals)) {
		if !envKeyRegexp.MatchString(k) {
			return &ResolutionError{
				Code: ErrCodeInvalidEnvKey,
				Message: fmt.Sprintf(
					"invalid environment variable name %q in %s: must match %s",
					k, origin, EnvKeyPattern,
				),
			}
		}
	}
	return nil
}

func parentRuntime(appSpec *Spec, env EnvIdentity, specEnv *Environment, resolved *ResolvedSpec, from string) (*ResolvedRuntimeEnvironment, error) {
	if prc, ok := resolved.Components[from]; ok {
		parentRT := prc.Runtime
		return &parentRT, nil
	}
	parentComp, ok := appSpec.Components[from]
	if !ok {
		// Validation already rejects unknown from. Display resolve still
		// attaches runtime on a partial spec, so treat this as no parent.
		return nil, nil //nolint:nilnil // absent parent is not an error; callers check for nil
	}
	rt, _, _, err := resolveRuntimeEnvironment(runtimeEnvRequest{
		specDir:    appSpec.SpecDir,
		envName:    env.Original,
		env:        specEnv,
		entityName: from,
		entityKind: "component",
		envFile:    parentComp.EnvFile,
		explicit:   parentComp.Env,
	})
	if err != nil {
		return nil, fmt.Errorf("parent %s: %w", from, err)
	}
	return &rt, nil
}

func specEnvironment(appSpec *Spec, envName string) *Environment {
	if appSpec == nil || len(appSpec.Environments) == 0 {
		return &Environment{}
	}
	if matched, ok := matchEnvKey(envName, slices.Collect(maps.Keys(appSpec.Environments))); ok {
		env := appSpec.Environments[matched]
		return &env
	}
	return &Environment{}
}

func attachRuntimeEnvironments(appSpec *Spec, env EnvIdentity, resolved *ResolvedSpec, report *ResolutionReport) error {
	if appSpec == nil || resolved == nil {
		return nil
	}
	specEnv := specEnvironment(appSpec, env.Original)
	var fields []ResolvedField

	for _, name := range slices.Sorted(maps.Keys(resolved.Components)) {
		comp := appSpec.Components[name]
		rt, fileHist, explicitHist, err := resolveRuntimeEnvironment(runtimeEnvRequest{
			specDir:    appSpec.SpecDir,
			envName:    env.Original,
			env:        specEnv,
			entityName: name,
			entityKind: "component",
			envFile:    comp.EnvFile,
			explicit:   comp.Env,
		})
		if err != nil {
			return fmt.Errorf("component %s: %w", name, err)
		}
		rc := resolved.Components[name]
		rc.Runtime = rt
		resolved.Components[name] = rc
		fields = append(fields, runtimeFields(name, fileHist, explicitHist)...)
	}

	for _, name := range slices.Sorted(maps.Keys(resolved.Tasks)) {
		raw := appSpec.Tasks[name]
		var parent *ResolvedRuntimeEnvironment
		if raw.From != "" {
			var err error
			parent, err = parentRuntime(appSpec, env, specEnv, resolved, raw.From)
			if err != nil {
				return fmt.Errorf("task %s: %w", name, err)
			}
		}
		rt, fileHist, explicitHist, err := resolveRuntimeEnvironment(runtimeEnvRequest{
			specDir:    appSpec.SpecDir,
			envName:    env.Original,
			env:        specEnv,
			entityName: name,
			entityKind: "task",
			envFile:    raw.EnvFile,
			explicit:   raw.Env,
			from:       raw.From,
			parent:     parent,
		})
		if err != nil {
			return fmt.Errorf("task %s: %w", name, err)
		}
		rtask := resolved.Tasks[name]
		rtask.Runtime = rt
		resolved.Tasks[name] = rtask
		fields = append(fields, runtimeFields(name, fileHist, explicitHist)...)
	}

	if report != nil {
		report.Fields = append(report.Fields, fields...)
	}
	return nil
}

func runtimeFields(entity string, fileHist, explicitHist []runtimeKeyHistory) []ResolvedField {
	out := make([]ResolvedField, 0, len(fileHist)+len(explicitHist))
	for _, h := range fileHist {
		out = append(out, ResolvedField{
			Component: entity,
			Path:      "runtime.fileValues." + h.key,
			Value:     h.value,
			Source:    joinHistory(h.sources),
		})
	}
	for _, h := range explicitHist {
		out = append(out, ResolvedField{
			Component: entity,
			Path:      "runtime.explicitValues." + h.key,
			Value:     h.value,
			Source:    joinHistory(h.sources),
		})
	}
	return out
}

func joinHistory(sources []string) string {
	return strings.Join(sources, ", ")
}
