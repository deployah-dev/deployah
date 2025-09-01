# Fuzzy Log Finder Feature Plan

## Overview

This document outlines the plan to implement fuzzy finder functionality for stream logs in Deployah using [fzf](https://github.com/junegunn/fzf). The feature will allow users to interactively search and filter real-time log streams with powerful fuzzy matching capabilities.

## Background

Currently, Deployah's `logs` command uses [Stern](https://github.com/stern/stern) to stream logs from Kubernetes pods. While Stern provides excellent multi-pod log aggregation, it lacks interactive search capabilities. By integrating fzf, users will be able to:

- Search through real-time log streams with fuzzy matching
- Filter logs by text patterns, timestamps, or log levels
- Navigate through large log volumes efficiently
- Execute actions on selected log entries

## Key Benefits

1. **Interactive Search**: Real-time fuzzy search through streaming logs
2. **Memory Management**: Efficient handling of large log volumes with `--tail` option
3. **Enhanced UX**: Better log navigation and filtering capabilities
4. **Action Integration**: Execute commands on selected log entries
5. **Color Preservation**: Maintain Stern's color-coded output for different pods

## Technical Requirements

### Dependencies

- **fzf**: Fuzzy finder utility (must be installed on user's system)
- **Stern**: Already integrated for log streaming
- **Go**: For implementing the integration logic

### System Requirements

- fzf version 0.20.0 or higher
- Unix-like system (Linux, macOS)
- Terminal with ANSI color support

## Feature Design

### 1. Enhanced `logs` Command with Fuzzy Finder

Add fuzzy finder functionality to the existing `logs` command using a new flag:

```bash
deployah logs [release-name] --fuzzy [flags]
```

### 2. Core Features

#### 2.1 Basic Fuzzy Search
- Real-time fuzzy matching on log content
- Support for exact and fuzzy search modes
- Case-insensitive search by default
- Configurable search sensitivity

#### 2.2 Memory Management
- Use fzf's `--tail` option to limit memory usage
- Configurable buffer size (default: 100,000 lines)
- Automatic cleanup of old log entries

#### 2.3 Enhanced Display
- Preserve Stern's color-coded output with `--ansi` flag
- Show newest logs at the top with `--tac` flag
- Prevent line truncation with `--wrap` flag
- Custom headers with usage instructions

#### 2.4 Interactive Actions
- **Enter**: Execute kubectl exec on selected pod
- **Ctrl-O**: Open selected log entry in editor (vim/nano)
- **Ctrl-R**: Refresh log stream
- **Ctrl-F**: Toggle search mode (fuzzy/exact)
- **Ctrl-C**: Exit fuzzy finder

### 3. Configuration Options

#### 3.1 Search Configuration
```bash
--fuzzy-search-mode string     Search mode: fuzzy, exact, regex (default "fuzzy")
--fuzzy-case-sensitive         Enable case-sensitive search
--fuzzy-search-delay int       Search input delay in milliseconds (default 100)
```

#### 3.2 Display Configuration
```bash
--fuzzy-buffer-size int        Maximum number of log lines to keep in memory (default 100000)
--fuzzy-header string          Custom header text for the fuzzy finder
--fuzzy-no-colors             Disable color output
--fuzzy-no-wrap               Disable line wrapping
```

#### 3.3 Action Configuration
```bash
--fuzzy-exec-action string     Action to execute on Enter key: exec, edit, copy (default "exec")
--fuzzy-editor string          Editor to use for log viewing (default "vim")
```

## Implementation Plan

### Phase 1: Basic Integration (Week 1-2)

1. **Enhance existing logs command**
   - Add `--fuzzy` flag to existing `logs` command
   - Implement basic fzf integration logic
   - Add fzf-specific flags with `fuzzy-` prefix

2. **Basic fzf integration**
   - Implement pipe from Stern to fzf
   - Add basic fzf options (`--tail`, `--tac`, `--wrap`, `--ansi`)
   - Handle fzf process lifecycle

3. **Error handling**
   - Check for fzf availability
   - Handle fzf process failures
   - Graceful fallback to regular logs command

### Phase 2: Enhanced Features (Week 3-4)

1. **Search functionality**
   - Implement search mode switching
   - Add case sensitivity options
   - Support for regex search mode

2. **Interactive actions**
   - Implement kubectl exec integration
   - Add log editing functionality
   - Support for custom actions

3. **Configuration management**
   - Add configuration file support
   - Implement command-line options
   - Add environment variable support

### Phase 3: Advanced Features (Week 5-6)

1. **Performance optimization**
   - Optimize memory usage
   - Implement efficient log buffering
   - Add performance monitoring

2. **Advanced filtering**
   - Timestamp-based filtering
   - Log level filtering
   - Pod/container-specific filtering

3. **User experience improvements**
   - Better error messages
   - Usage examples and help text
   - Integration with existing logging system

## Technical Implementation Details

### 1. Enhanced Logs Command Structure

```go
// internal/cmd/logs.go - Add to existing NewLogsCommand function
func NewLogsCommand() *cobra.Command {
    logsCommand := &cobra.Command{
        Use:   "logs [release-name]",
        Short: "View logs for a deployed release",
        Long:  `View logs from pods associated with a deployed release...`,
        Args:  cobra.MaximumNArgs(1),
        RunE:  runLogs,
    }
    
    // Existing flags...
    
    // Add fuzzy finder flags
    logsCommand.Flags().Bool("fuzzy", false, "Enable fuzzy finder interface for log searching")
    logsCommand.Flags().String("fuzzy-search-mode", "fuzzy", "Search mode: fuzzy, exact, regex")
    logsCommand.Flags().Bool("fuzzy-case-sensitive", false, "Enable case-sensitive search")
    logsCommand.Flags().Int("fuzzy-search-delay", 100, "Search input delay in milliseconds")
    logsCommand.Flags().Int("fuzzy-buffer-size", 100000, "Maximum number of log lines to keep in memory")
    logsCommand.Flags().String("fuzzy-header", "", "Custom header text for the fuzzy finder")
    logsCommand.Flags().Bool("fuzzy-no-colors", false, "Disable color output")
    logsCommand.Flags().Bool("fuzzy-no-wrap", false, "Disable line wrapping")
    logsCommand.Flags().String("fuzzy-exec-action", "exec", "Action to execute on Enter key: exec, edit, copy")
    logsCommand.Flags().String("fuzzy-editor", "vim", "Editor to use for log viewing")
    
    return logsCommand
}
```

### 2. fzf Integration in runLogs

```go
func runLogs(cmd *cobra.Command, args []string) error {
    // Existing logic...
    
    fuzzy, _ := cmd.Flags().GetBool("fuzzy")
    
    if fuzzy {
        return runLogsWithFuzzy(cmd, args, cfg)
    }
    
    // Existing Stern execution...
    if err := sternpkg.Run(cmd.Context(), k8sClient, cfg); err != nil {
        return fmt.Errorf("stern run failed: %w", err)
    }
    return nil
}

func runLogsWithFuzzy(cmd *cobra.Command, args []string, sternConfig *sternpkg.Config) error {
    // Check fzf availability
    if err := checkFzfAvailability(); err != nil {
        return fmt.Errorf("fzf not available: %w", err)
    }
    
    // Create fzf process with appropriate options
    fzfCmd := createFzfCommand(cmd)
    
    // Pipe Stern output to fzf
    return pipeSternToFzf(sternConfig, fzfCmd)
}
```

### 3. Memory Management

```go
func createFzfCommand(cmd *cobra.Command) *exec.Cmd {
    bufferSize, _ := cmd.Flags().GetInt("fuzzy-buffer-size")
    
    args := []string{
        "--tail", strconv.Itoa(bufferSize),
        "--tac",
        "--wrap",
        "--ansi",
        "--no-sort",
        "--exact",
    }
    
    // Add custom bindings
    args = append(args, buildFzfBindings(cmd)...)
    
    return exec.Command("fzf", args...)
}
```

### 4. Interactive Actions

```go
func buildFzfBindings(cmd *cobra.Command) []string {
    execAction, _ := cmd.Flags().GetString("fuzzy-exec-action")
    editor, _ := cmd.Flags().GetString("fuzzy-editor")
    
    bindings := []string{
        "--bind", "ctrl-o:execute:" + editor + " -n <(kubectl logs {1})",
        "--bind", "enter:execute:kubectl exec -it {1} -- bash",
        "--bind", "ctrl-r:reload:stern . --color always",
        "--bind", "ctrl-f:toggle-search",
    }
    
    return bindings
}
```

## Configuration File Support

### 1. Configuration Structure

```yaml
# ~/.config/deployah/logs.yaml
logs:
  fuzzy:
    buffer-size: 100000
    search-mode: fuzzy
    case-sensitive: false
    search-delay: 100
    editor: vim
    exec-action: exec
    colors: true
    wrap: true
    header: "╱ Enter (kubectl exec) ╱ CTRL-O (open log in vim) ╱"
```

### 2. Environment Variables

```bash
DEPLOYAH_LOGS_FUZZY_BUFFER_SIZE=100000
DEPLOYAH_LOGS_FUZZY_SEARCH_MODE=fuzzy
DEPLOYAH_LOGS_FUZZY_EDITOR=vim
DEPLOYAH_LOGS_FUZZY_EXEC_ACTION=exec
```

## Usage Examples

### 1. Basic Usage

```bash
# View logs with fuzzy finder
deployah logs myapp --fuzzy

# Search for error logs
deployah logs myapp --fuzzy --fuzzy-search-mode exact

# Use custom buffer size
deployah logs myapp --fuzzy --fuzzy-buffer-size 50000
```

### 2. Advanced Usage

```bash
# Case-sensitive search
deployah logs myapp --fuzzy --fuzzy-case-sensitive

# Use different editor
deployah logs myapp --fuzzy --fuzzy-editor nano

# Custom header
deployah logs myapp --fuzzy --fuzzy-header "Search logs for myapp"
```

### 3. Integration with Existing Features

```bash
# Use with component filtering
deployah logs myapp --component api --fuzzy

# Use with environment filtering
deployah logs myapp --environment prod --fuzzy

# Use with custom selectors
deployah logs myapp --selector "app=myapp,env=prod" --fuzzy
```

## Testing Strategy

### 1. Unit Tests

- Test fzf availability checking
- Test configuration parsing
- Test command-line argument handling
- Test error scenarios

### 2. Integration Tests

- Test Stern to fzf piping
- Test interactive actions
- Test memory management
- Test color preservation

### 3. End-to-End Tests

- Test complete workflow
- Test with real Kubernetes clusters
- Test performance with large log volumes
- Test user experience scenarios

## Performance Considerations

### 1. Memory Usage

- Monitor memory consumption with large log volumes
- Implement efficient log buffering
- Use fzf's `--tail` option appropriately
- Consider implementing log rotation

### 2. CPU Usage

- Optimize search performance
- Implement efficient filtering
- Monitor fzf process resource usage
- Consider background processing for heavy operations

### 3. Network Usage

- Minimize Kubernetes API calls
- Implement efficient log streaming
- Consider log caching strategies
- Monitor network bandwidth usage

## Security Considerations

### 1. Command Execution

- Validate kubectl commands before execution
- Sanitize user input for command generation
- Implement proper error handling for command failures
- Consider command execution timeouts

### 2. File Access

- Validate file paths for template files
- Implement proper file permission checks
- Sanitize file content before processing
- Consider sandboxing for file operations

### 3. Kubernetes Access

- Validate Kubernetes resource access
- Implement proper RBAC checks
- Monitor Kubernetes API usage
- Consider access token management

## Future Enhancements

### 1. Advanced Search Features

- Multi-line log entry support
- Structured log parsing (JSON, etc.)
- Log level-based filtering
- Timestamp range filtering

### 2. Enhanced Actions

- Log export functionality
- Log analysis tools integration
- Custom action plugins
- Batch operations on selected logs

### 3. UI Improvements

- Custom fzf themes
- Better visual indicators
- Keyboard shortcut customization
- Mouse support

### 4. Integration Features

- IDE/editor integration
- Web interface support
- API endpoints for programmatic access
- Plugin system for custom extensions

## Migration Strategy

### 1. Backward Compatibility

- Keep existing `logs` command behavior unchanged
- Add `--fuzzy` flag as an enhancement
- Maintain consistent flag naming
- Provide migration documentation

### 2. Feature Parity

- Ensure all existing `logs` features work with `--fuzzy`
- Maintain consistent behavior for common operations
- Provide feature comparison documentation
- Consider deprecation timeline for old command

### 3. User Education

- Create comprehensive documentation
- Provide usage examples and tutorials
- Create video demonstrations
- Offer training sessions

## Success Metrics

### 1. User Adoption

- Track command usage statistics
- Monitor user feedback and satisfaction
- Measure feature adoption rate
- Track support ticket reduction

### 2. Performance Metrics

- Monitor memory usage patterns
- Track search performance
- Measure user interaction efficiency
- Monitor system resource usage

### 3. Quality Metrics

- Track bug reports and fixes
- Monitor feature request frequency
- Measure user satisfaction scores
- Track documentation usage

## Conclusion

The fuzzy log finder feature will significantly enhance Deployah's log viewing capabilities by providing an interactive, efficient, and user-friendly interface for searching through Kubernetes logs. The implementation plan ensures a robust, performant, and maintainable solution that integrates seamlessly with the existing codebase while providing powerful new functionality to users.

The phased approach allows for incremental development and testing, ensuring that each component is thoroughly validated before moving to the next phase. The comprehensive testing strategy and performance considerations ensure that the feature will be reliable and efficient in production environments.

## References

- [fzf Documentation](https://github.com/junegunn/fzf)
- [fzf Browsing Log Streams Guide](https://junegunn.github.io/fzf/tips/browsing-log-streams/)
- [Stern Documentation](https://github.com/stern/stern)
- [Deployah Current Logs Implementation](internal/cmd/logs.go)
