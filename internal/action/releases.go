package action

import (
	"deployah.dev/deployah/internal/spec"

	v1 "helm.sh/helm/v4/pkg/release/v1"
)

// matchingReleases drops nil entries. When environment is a wildcard
// instance such as review/pr-123, it keeps only that Helm release so a
// sibling review/pr-456 is not included. Logical names such as review
// keep every release that shared the environment label.
//
// Project may be empty (deployah list --environment review/pr-123). In
// that case the project for [spec.EnvIdentity.ReleaseName] is read from
// each candidate's [spec.LabelProject] label.
func matchingReleases(releases []*v1.Release, project, environment string) []*v1.Release {
	env := spec.NormalizeEnv(environment)
	exact := environment != "" && env.IsWildcard()
	valid := make([]*v1.Release, 0, len(releases))
	for _, r := range releases {
		if r == nil {
			continue
		}
		if !exact {
			valid = append(valid, r)
			continue
		}
		p := project
		if p == "" && r.Labels != nil {
			p = r.Labels[spec.LabelProject]
		}
		if p == "" {
			continue
		}
		if r.Name != env.ReleaseName(p) {
			continue
		}
		valid = append(valid, r)
	}
	return valid
}
