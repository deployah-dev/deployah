// Copyright 2025 The Deployah Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing the License.

package run

import (
	"context"
	"fmt"
	"time"

	"k8s.io/client-go/kubernetes"
	"nabat.dev/nabat"

	"deployah.dev/deployah/internal/cmd/cmdopts"
	"deployah.dev/deployah/internal/k8s"
	"deployah.dev/deployah/internal/session"
	"deployah.dev/deployah/internal/spec"

	batchv1 "k8s.io/api/batch/v1"
)

// Options holds command-line flags for run.
type Options struct {
	Task        string `nabat:"task"`
	Environment string `nabat:"environment"`
	Detach      bool   `nabat:"detach"`
	Count       int    `nabat:"count"`
	Parallelism int    `nabat:"parallelism"`
	Yes         bool   `nabat:"yes"`
}

// Register adds the run command to app.
func Register(app *nabat.App) {
	app.MustCommand("run",
		nabat.WithDescription("Run a spec task as a one-off Job"),
		nabat.WithLongDescription("Create a Kubernetes Job for a task from the spec. Works for preDeploy, postDeploy, and manual tasks. Runs only the named task; tasks listed in its after field are not run. Waits for completion unless --detach is set."),
		nabat.WithArg("task", "", nabat.WithRequired(), nabat.WithUsage("Task name to run"), nabat.WithPrompt("Task", "", nabat.WithHint("e.g. migrate, backfill"))),
		nabat.WithArg("environment", "", nabat.WithRequired(), nabat.WithUsage("Environment to run in"), nabat.WithPrompt("Environment", "", nabat.WithHint("e.g. prod, staging"))),
		nabat.WithFlag("detach", false, nabat.WithUsage("Return after creating the Job without waiting for completion")),
		nabat.WithFlag("count", 0, nabat.WithUsage("Override fanout count for this run")),
		nabat.WithFlag("parallelism", 0, nabat.WithUsage("Override how many copies may run at once")),
		nabat.WithFlag("yes", false, nabat.WithShort('y'), nabat.WithUsage("Run without an interactive confirmation prompt")),
		nabat.WithExample(`
# Run a manual backfill and wait for it to finish
deployah run backfill production

# Run without waiting
deployah run backfill production --detach

# Override fanout for this run
deployah run backfill production --count 4 --parallelism 2`),
		nabat.WithRun(runTask),
	)
}

func runTask(c *nabat.Context) error {
	opts := &Options{}
	if err := c.Bind(opts); err != nil {
		return fmt.Errorf("binding options: %w", err)
	}
	if opts.Count < 0 || opts.Parallelism < 0 {
		return fmt.Errorf("--count and --parallelism must be zero or positive")
	}
	if opts.Parallelism > spec.MaxFanoutParallelism {
		return fmt.Errorf("--parallelism must be at most %d", spec.MaxFanoutParallelism)
	}
	if opts.Count > 0 && opts.Parallelism > opts.Count {
		return fmt.Errorf("--parallelism must be less than or equal to --count")
	}

	sess := session.FromContext(c)

	platform, platformErr := sess.Platform()
	if platformErr != nil {
		return fmt.Errorf("load platform file: %w", platformErr)
	}

	manifest, err := spec.Load(c, sess.SpecPath(), opts.Environment, platform)
	if err != nil {
		return fmt.Errorf("load spec: %w", err)
	}

	rt, err := resolveRunTask(manifest, platform, opts.Environment, opts.Task)
	if err != nil {
		return err
	}

	if !opts.Detach {
		if toErr := spec.CheckTaskTimeout(opts.Task, rt.Task, sess.Timeout()); toErr != nil {
			return toErr
		}
	}

	confirmed, confirmErr := c.Confirm(
		fmt.Sprintf("Run task %q in %s?", opts.Task, opts.Environment),
		nabat.WithAffirmative("Yes, run it"),
		nabat.WithNegative("No, cancel"),
		nabat.WithYes(opts.Yes),
		nabat.WithBypassHint("--yes"),
	)
	if confirmErr != nil {
		return confirmErr
	}
	if !confirmed {
		c.Info("Run cancelled")
		return nil
	}

	cluster, err := sess.Target(c, opts.Environment)
	if err != nil {
		return fmt.Errorf("target cluster: %w", err)
	}
	cmdopts.WarnContextFallback(c, cluster, opts.Environment)

	cs, err := cluster.Kubernetes()
	if err != nil {
		return fmt.Errorf("kubernetes client: %w", err)
	}

	job, err := k8s.BuildTaskJob(k8s.TaskJobOptions{
		Project:     manifest.Project,
		Environment: opts.Environment,
		Namespace:   cluster.Namespace(),
		TaskName:    opts.Task,
		Task:        rt.Task,
		Count:       opts.Count,
		Parallelism: opts.Parallelism,
		Profile:     rt.MergedProfile,
	})
	if err != nil {
		return fmt.Errorf("build job for %s: %w", opts.Task, err)
	}

	return executeRun(c, cs, sess.Timeout(), job, opts.Detach)
}

func executeRun(c *nabat.Context, cs kubernetes.Interface, timeout time.Duration, job *batchv1.Job, detach bool) error {
	created, err := k8s.CreateTaskJob(c, cs, job)
	if err != nil {
		return err
	}
	c.Success("Created Job", "name", created.Name, "namespace", created.Namespace)

	if detach {
		c.Info("Detached; the Job continues in the cluster")
		return nil
	}

	waitCtx, cancel := context.WithTimeout(c, timeout)
	defer cancel()
	if waitErr := k8s.WaitForJob(waitCtx, cs, created.Namespace, created.Name); waitErr != nil {
		return waitErr
	}
	c.Success("Job completed", "name", created.Name)
	return nil
}

func resolveRunTask(manifest *spec.Spec, platform *spec.PlatformConfig, environment, name string) (spec.ResolvedTask, error) {
	envIdentity := spec.NormalizeEnv(environment)
	if platform != nil {
		resolved, _, err := spec.Resolve(manifest, platform, envIdentity, spec.SubstitutionReport{})
		if err != nil {
			return spec.ResolvedTask{}, fmt.Errorf("resolve spec: %w", err)
		}
		rt, ok := resolved.Tasks[name]
		if !ok {
			if _, exists := manifest.Tasks[name]; exists {
				return spec.ResolvedTask{}, fmt.Errorf("task %s is skipped in environment %s", name, environment)
			}
			return spec.ResolvedTask{}, fmt.Errorf("unknown task %s", name)
		}
		return rt, nil
	}

	merged, ok := manifest.MergedTask(name)
	if !ok {
		return spec.ResolvedTask{}, fmt.Errorf("unknown task %s", name)
	}
	if len(merged.Profiles) > 0 {
		return spec.ResolvedTask{}, fmt.Errorf("task %s sets profiles but no platform file was found", name)
	}
	if len(merged.Environments) > 0 {
		if _, match := spec.MatchEnvKey(environment, merged.Environments); !match {
			return spec.ResolvedTask{}, fmt.Errorf("task %s is skipped in environment %s", name, environment)
		}
	}
	return spec.ResolvedTask{Task: merged}, nil
}
