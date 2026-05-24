package shell

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"slices"
	"strings"
	"syscall"

	"github.com/deployah-dev/deployah/internal/k8s"
	"github.com/deployah-dev/deployah/internal/runtime"
	"golang.org/x/term"
	"nabat.dev/nabat"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"
)

// terminalSizeQueue implements remotecommand.TerminalSizeQueue
type terminalSizeQueue struct {
	ch chan remotecommand.TerminalSize
}

func (q *terminalSizeQueue) Next() *remotecommand.TerminalSize {
	sz, ok := <-q.ch
	if !ok {
		return nil
	}
	return &sz
}

// ExecuteOptions contains all the options for shell execution
type ExecuteOptions struct {
	ProjectName string
	Component   string
	Container   string
	Shell       string
	Command     string
	WorkDir     string
	Environment string
}

// ShellExecutor handles shell execution in containers
type ShellExecutor struct {
	runtime *runtime.Runtime
	k8s     *k8s.Client
	ctx     *nabat.Context
}

// NewShellExecutor creates a new shell executor
func NewShellExecutor(rt *runtime.Runtime, ctx *nabat.Context) (*ShellExecutor, error) {
	k8sClient, err := k8s.NewClientFromRuntime(ctx, rt)
	if err != nil {
		return nil, fmt.Errorf("failed to create k8s client: %w", err)
	}

	return &ShellExecutor{
		runtime: rt,
		k8s:     k8sClient,
		ctx:     ctx,
	}, nil
}

// Execute runs the shell command with the given options
func (e *ShellExecutor) Execute(opts ExecuteOptions) error {
	componentName, err := e.selectComponent(opts.ProjectName, opts.Component)
	if err != nil {
		return fmt.Errorf("failed to select component: %w", err)
	}

	environmentName, err := e.selectEnvironment(opts.ProjectName, componentName, opts.Environment)
	if err != nil {
		return fmt.Errorf("failed to select environment: %w", err)
	}

	pods, err := e.k8s.GetRunningPods(e.ctx, opts.ProjectName, componentName, environmentName)
	if err != nil {
		return fmt.Errorf("failed to get running pods: %w", err)
	}

	if len(pods) == 0 {
		return fmt.Errorf("no running pods found for project '%s' component '%s' environment '%s'",
			opts.ProjectName, componentName, environmentName)
	}

	pod := pods[0]

	containerName, err := e.selectContainer(pod.Containers, componentName, opts.Container)
	if err != nil {
		return fmt.Errorf("failed to select container: %w", err)
	}

	availableShells, err := e.detectAvailableShells(pod.Name, containerName)
	if err != nil {
		return fmt.Errorf("failed to detect available shells: %w", err)
	}

	var selectedShell string
	if opts.Shell != "" {
		selectedShell = opts.Shell
		if len(availableShells) > 0 && !slices.Contains(availableShells, opts.Shell) {
			fmt.Fprintf(e.ctx.IO().ErrOut, "Warning: Preferred shell '%s' not detected, but will try it anyway\n", opts.Shell)
		}
	} else {
		if len(availableShells) == 0 {
			return fmt.Errorf("no shells found and no shell specified")
		}
		selectedShell = availableShells[0]
	}

	execCommand := e.buildExecCommand(selectedShell, opts.Command, opts.WorkDir)

	return e.execInContainer(pod.Name, containerName, execCommand)
}

// selectComponent selects the component to connect to
func (e *ShellExecutor) selectComponent(projectName, componentName string) (string, error) {
	if componentName != "" {
		err := e.k8s.ValidateComponentExists(e.ctx, projectName, componentName)
		if err != nil {
			return "", err
		}
		return componentName, nil
	}

	components, err := e.k8s.GetAvailableComponents(e.ctx, projectName)
	if err != nil {
		return "", fmt.Errorf("failed to get available components: %w", err)
	}

	if len(components) == 1 {
		return components[0], nil
	}

	return selectComponentInteractively(e.ctx, components)
}

// selectEnvironment selects the environment to connect to
func (e *ShellExecutor) selectEnvironment(projectName, componentName, environmentName string) (string, error) {
	if environmentName != "" {
		pods, err := e.k8s.GetRunningPods(e.ctx, projectName, componentName, environmentName)
		if err != nil {
			return "", fmt.Errorf("failed to validate environment '%s': %w", environmentName, err)
		}
		if len(pods) == 0 {
			return "", fmt.Errorf("environment '%s' not found or has no running pods for project '%s' component '%s'", environmentName, projectName, componentName)
		}
		return environmentName, nil
	}

	environments, err := e.k8s.GetAvailableEnvironments(e.ctx, projectName, componentName)
	if err != nil {
		return "", fmt.Errorf("failed to get available environments: %w", err)
	}

	if len(environments) == 1 {
		return environments[0], nil
	}

	return selectEnvironmentInteractively(e.ctx, environments, projectName, componentName)
}

// selectContainer selects a container from the list
func (e *ShellExecutor) selectContainer(containers []string, componentName, containerName string) (string, error) {
	if containerName != "" {
		for _, container := range containers {
			if container == containerName {
				return container, nil
			}
		}
		return "", fmt.Errorf("container '%s' not found in pod", containerName)
	}

	if len(containers) == 1 {
		return containers[0], nil
	}

	for _, container := range containers {
		if container == componentName {
			return container, nil
		}
	}

	return selectContainerInteractively(e.ctx, containers, componentName)
}

// detectAvailableShells detects which shells are available in the container
func (e *ShellExecutor) detectAvailableShells(podName, containerName string) ([]string, error) {
	availableShells := []string{}
	commonShells := []string{"bash", "ash", "zsh", "fish", "sh"}

	for _, shell := range commonShells {
		if err := e.execTest(podName, containerName, []string{shell, "-c", "exit 0"}); err == nil {
			availableShells = append(availableShells, shell)
		}
	}

	return availableShells, nil
}

// buildExecCommand builds the command to execute
func (e *ShellExecutor) buildExecCommand(shell, command, workdir string) []string {
	if command != "" {
		return []string{shell, "-c", command}
	}

	cmd := []string{shell}

	if workdir != "" {
		cmd = append(cmd, "-c", fmt.Sprintf("cd %s && exec %s", workdir, shell))
	}

	return cmd
}

// execTest executes a test command to check if something exists
func (e *ShellExecutor) execTest(podName, containerName string, cmd []string) error {
	k8sClient, err := e.runtime.Kubernetes()
	if err != nil {
		return fmt.Errorf("failed to get kubernetes client: %w", err)
	}

	config, err := e.runtime.RESTConfig()
	if err != nil {
		return fmt.Errorf("failed to get rest config: %w", err)
	}

	req := k8sClient.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(podName).
		Namespace(e.runtime.Namespace()).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: containerName,
			Command:   cmd,
			Stdin:     false,
			Stdout:    true,
			Stderr:    true,
			TTY:       false,
		}, scheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(config, "POST", req.URL())
	if err != nil {
		return fmt.Errorf("failed to create executor: %w", err)
	}

	return exec.StreamWithContext(e.ctx, remotecommand.StreamOptions{
		Stdin:  nil,
		Stdout: io.Discard,
		Stderr: io.Discard,
		Tty:    false,
	})
}

// execInContainer executes a command in the container
func (e *ShellExecutor) execInContainer(podName, containerName string, cmd []string) error {
	k8sClient, err := e.runtime.Kubernetes()
	if err != nil {
		return fmt.Errorf("failed to get kubernetes client: %w", err)
	}

	config, err := e.runtime.RESTConfig()
	if err != nil {
		return fmt.Errorf("failed to get rest config: %w", err)
	}

	streams := e.ctx.IO()

	fd := int(os.Stdin.Fd())
	isTTY := term.IsTerminal(fd)
	var oldState *term.State
	if isTTY {
		oldState, err = term.MakeRaw(fd)
		if err != nil {
			return fmt.Errorf("failed to set terminal to raw mode: %w", err)
		}
		defer func() { _ = term.Restore(fd, oldState) }()
	}

	var sizeQueue remotecommand.TerminalSizeQueue
	var resizeCh chan os.Signal
	var q *terminalSizeQueue
	if isTTY {
		q = &terminalSizeQueue{ch: make(chan remotecommand.TerminalSize, 1)}
		sizeQueue = q

		if w, h, err := term.GetSize(fd); err == nil {
			q.ch <- remotecommand.TerminalSize{Width: uint16(w), Height: uint16(h)}
		}

		resizeCh = make(chan os.Signal, 1)
		signal.Notify(resizeCh, syscall.SIGWINCH)
		go func() {
			for range resizeCh {
				if w, h, err := term.GetSize(fd); err == nil {
					q.ch <- remotecommand.TerminalSize{Width: uint16(w), Height: uint16(h)}
				}
			}
		}()
		defer func() {
			signal.Stop(resizeCh)
			close(resizeCh)
			close(q.ch)
		}()
	}

	req := k8sClient.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(podName).
		Namespace(e.runtime.Namespace()).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: containerName,
			Command:   cmd,
			Stdin:     true,
			Stdout:    true,
			Stderr:    true,
			TTY:       true,
		}, scheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(config, "POST", req.URL())
	if err != nil {
		return fmt.Errorf("failed to create executor: %w", err)
	}

	err = exec.StreamWithContext(e.ctx, remotecommand.StreamOptions{
		Stdin:             streams.In,
		Stdout:            streams.Out,
		Stderr:            streams.ErrOut,
		Tty:               true,
		TerminalSizeQueue: sizeQueue,
	})

	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}

		if e.ctx.Err() != nil {
			return nil
		}

		// The SPDY executor stringifies syscall.EPIPE before returning it, so
		// errors.Is(err, syscall.EPIPE) is tried first and the string fallback
		// handles cases where the errno is not preserved through the stream layer.
		if errors.Is(err, syscall.EPIPE) || strings.Contains(err.Error(), "broken pipe") {
			return nil
		}

		return err
	}

	return nil
}
