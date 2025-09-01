package shell

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"slices"
	"strings"
	"syscall"

	"github.com/deployah-dev/deployah/internal/k8s"
	"github.com/deployah-dev/deployah/internal/runtime"
	"github.com/spf13/cobra"
	"golang.org/x/term"
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
	cmd     *cobra.Command
}

// NewShellExecutor creates a new shell executor
func NewShellExecutor(rt *runtime.Runtime, cmd *cobra.Command) (*ShellExecutor, error) {
	k8sClient, err := k8s.NewClientFromRuntime(cmd.Context(), rt)
	if err != nil {
		return nil, fmt.Errorf("failed to create k8s client: %w", err)
	}

	return &ShellExecutor{
		runtime: rt,
		k8s:     k8sClient,
		cmd:     cmd,
	}, nil
}

// Execute runs the shell command with the given options
func (e *ShellExecutor) Execute(opts ExecuteOptions) error {
	// 1. Select component if not specified
	componentName, err := e.selectComponent(opts.ProjectName, opts.Component)
	if err != nil {
		return fmt.Errorf("failed to select component: %w", err)
	}

	// 2. Select environment if not specified
	environmentName, err := e.selectEnvironment(opts.ProjectName, componentName, opts.Environment)
	if err != nil {
		return fmt.Errorf("failed to select environment: %w", err)
	}

	// 3. Get running pods for the project, component, and environment
	pods, err := e.k8s.GetRunningPods(e.cmd.Context(), opts.ProjectName, componentName, environmentName)
	if err != nil {
		return fmt.Errorf("failed to get running pods: %w", err)
	}

	if len(pods) == 0 {
		return fmt.Errorf("no running pods found for project '%s' component '%s' environment '%s'",
			opts.ProjectName, componentName, environmentName)
	}

	// 4. Just use the first available pod (they're all equivalent)
	pod := pods[0]

	// 5. Select container (if multiple)
	containerName, err := e.selectContainer(pod.Containers, componentName, opts.Container)
	if err != nil {
		return fmt.Errorf("failed to select container: %w", err)
	}

	// 6. Detect available shells
	availableShells, err := e.detectAvailableShells(pod.Name, containerName)
	if err != nil {
		return fmt.Errorf("failed to detect available shells: %w", err)
	}

	// 7. Select shell
	var selectedShell string
	if opts.Shell != "" {
		// User specified a shell
		if len(availableShells) > 0 && slices.Contains(availableShells, opts.Shell) {
			selectedShell = opts.Shell
		} else {
			// User's preferred shell not found, but we'll try it anyway
			selectedShell = opts.Shell
			if len(availableShells) > 0 {
				fmt.Fprintf(os.Stderr, "Warning: Preferred shell '%s' not detected, but will try it anyway\n", opts.Shell)
			}
		}
	} else {
		// No shell specified by user
		if len(availableShells) == 0 {
			return fmt.Errorf("no shells found and no shell specified")
		}
		selectedShell = availableShells[0]
	}

	// 8. Build command to execute
	execCommand := e.buildExecCommand(selectedShell, opts.Command, opts.WorkDir)

	// 9. Execute the command
	return e.execInContainer(pod.Name, containerName, execCommand)
}

// selectComponent selects the component to connect to
func (e *ShellExecutor) selectComponent(projectName, componentName string) (string, error) {
	// If component is specified, validate it exists
	if componentName != "" {
		err := e.k8s.ValidateComponentExists(e.cmd.Context(), projectName, componentName)
		if err != nil {
			return "", err
		}
		return componentName, nil
	}

	// Get all available components from running pods for this project
	components, err := e.k8s.GetAvailableComponents(e.cmd.Context(), projectName)
	if err != nil {
		return "", fmt.Errorf("failed to get available components: %w", err)
	}

	// Auto-select if only one component
	if len(components) == 1 {
		return components[0], nil
	}

	// Multiple components - show interactive selection
	return selectComponentInteractively(components)
}

// selectEnvironment selects the environment to connect to
func (e *ShellExecutor) selectEnvironment(projectName, componentName, environmentName string) (string, error) {
	// If environment is specified, validate it exists
	if environmentName != "" {
		pods, err := e.k8s.GetRunningPods(e.cmd.Context(), projectName, componentName, environmentName)
		if err != nil {
			return "", fmt.Errorf("failed to validate environment '%s': %w", environmentName, err)
		}
		if len(pods) == 0 {
			return "", fmt.Errorf("environment '%s' not found or has no running pods for project '%s' component '%s'", environmentName, projectName, componentName)
		}
		return environmentName, nil
	}

	// Get all available environments from running pods for this project and component
	environments, err := e.k8s.GetAvailableEnvironments(e.cmd.Context(), projectName, componentName)
	if err != nil {
		return "", fmt.Errorf("failed to get available environments: %w", err)
	}

	// Auto-select if only one environment
	if len(environments) == 1 {
		return environments[0], nil
	}

	// Multiple environments - show interactive selection
	return selectEnvironmentInteractively(environments, projectName, componentName)
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

	// Auto-select if only one container
	if len(containers) == 1 {
		return containers[0], nil
	}

	// Try to match component name
	for _, container := range containers {
		if container == componentName {
			return container, nil
		}
	}

	// Multiple containers - show interactive selection
	return selectContainerInteractively(containers, componentName)
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
	// Get Kubernetes client
	k8sClient, err := e.runtime.Kubernetes()
	if err != nil {
		return fmt.Errorf("failed to get kubernetes client: %w", err)
	}

	// Get REST config
	config, err := e.runtime.RESTConfig()
	if err != nil {
		return fmt.Errorf("failed to get rest config: %w", err)
	}

	// Create exec request for test command (no TTY for simple test)
	req := k8sClient.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(podName).
		Namespace(e.runtime.Namespace()).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: containerName,
			Command:   cmd,
			Stdin:     false,
			Stdout:    true, // Need at least one stream
			Stderr:    true, // Capture stderr for error detection
			TTY:       false,
		}, scheme.ParameterCodec)

	// Create SPDY executor
	exec, err := remotecommand.NewSPDYExecutor(config, "POST", req.URL())
	if err != nil {
		return fmt.Errorf("failed to create executor: %w", err)
	}

	// Execute the test command - if it returns non-zero, the shell doesn't exist
	// We need to provide at least one stream, so we'll use io.Discard to discard output
	return exec.StreamWithContext(e.cmd.Context(), remotecommand.StreamOptions{
		Stdin:  nil,
		Stdout: io.Discard, // Discard stdout
		Stderr: io.Discard, // Discard stderr
		Tty:    false,
	})
}

// execInContainer executes a command in the container
func (e *ShellExecutor) execInContainer(podName, containerName string, cmd []string) error {
	// Get Kubernetes client
	k8sClient, err := e.runtime.Kubernetes()
	if err != nil {
		return fmt.Errorf("failed to get kubernetes client: %w", err)
	}

	// Get REST config
	config, err := e.runtime.RESTConfig()
	if err != nil {
		return fmt.Errorf("failed to get rest config: %w", err)
	}

	// Put local terminal into raw mode so Ctrl+C is delivered to remote shell instead of canceling our context
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

	// Prepare resize handling
	var sizeQueue remotecommand.TerminalSizeQueue
	var resizeCh chan os.Signal
	var q *terminalSizeQueue
	if isTTY {
		q = &terminalSizeQueue{ch: make(chan remotecommand.TerminalSize, 1)}
		sizeQueue = q

		// Send initial size
		if w, h, err := term.GetSize(fd); err == nil {
			q.ch <- remotecommand.TerminalSize{Width: uint16(w), Height: uint16(h)}
		}

		// Watch SIGWINCH and push sizes
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

	// Create exec request
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

	// Create SPDY executor
	exec, err := remotecommand.NewSPDYExecutor(config, "POST", req.URL())
	if err != nil {
		return fmt.Errorf("failed to create executor: %w", err)
	}

	// Execute the command
	err = exec.StreamWithContext(e.cmd.Context(), remotecommand.StreamOptions{
		Stdin:             e.cmd.InOrStdin(),
		Stdout:            e.cmd.OutOrStdout(),
		Stderr:            e.cmd.ErrOrStderr(),
		Tty:               true,
		TerminalSizeQueue: sizeQueue,
	})

	// Handle EOF and exit gracefully - these are normal termination conditions
	if err != nil {
		// Check if it's an EOF error (Ctrl+D or normal stream end)
		if err == io.EOF {
			return nil // EOF is expected when user exits shell or presses Ctrl+D
		}

		// Check if it's a context cancellation (Ctrl+C)
		if e.cmd.Context().Err() != nil {
			return nil // Context cancellation is expected when user presses Ctrl+C
		}

		// Check for "broken pipe" errors which can occur when the remote shell terminates
		if strings.Contains(err.Error(), "broken pipe") || strings.Contains(err.Error(), "write: broken pipe") {
			return nil // Broken pipe is expected when remote shell terminates
		}

		// For other errors, return them as-is
		return err
	}

	return nil
}
