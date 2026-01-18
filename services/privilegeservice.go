package services

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// PrivilegeService handles elevated privilege execution
type PrivilegeService struct{}

// NewPrivilegeService creates a new privilege service
func NewPrivilegeService() (*PrivilegeService, error) {
	return &PrivilegeService{}, nil
}

// RunElevated executes a command with elevated privileges
func (p *PrivilegeService) RunElevated(command string, args []string) (string, error) {
	switch runtime.GOOS {
	case "windows":
		return p.runWindowsElevated(command, args)
	case "darwin":
		return p.runMacOSElevated(command, args)
	case "linux":
		return p.runLinuxElevated(command, args)
	default:
		return "", fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}

// runWindowsElevated executes command with UAC elevation (Windows)
func (p *PrivilegeService) runWindowsElevated(command string, args []string) (string, error) {
	// Build command line
	cmdLine := command
	if len(args) > 0 {
		cmdLine += " " + strings.Join(args, " ")
	}

	// Use PowerShell Start-Process with -Verb RunAs
	psScript := fmt.Sprintf(`Start-Process -FilePath '%s' -ArgumentList '%s' -Verb RunAs -Wait -WindowStyle Hidden`,
		command, strings.Join(args, "', '"))

	cmd := hideWindowCmd("powershell.exe", "-ExecutionPolicy", "Bypass", "-Command", psScript)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("failed to run elevated command: %w", err)
	}

	return string(output), nil
}

// runMacOSElevated executes command with administrator privileges (macOS)
func (p *PrivilegeService) runMacOSElevated(command string, args []string) (string, error) {
	// Build command line with proper escaping
	cmdLine := command
	if len(args) > 0 {
		// Escape single quotes in arguments
		escapedArgs := make([]string, len(args))
		for i, arg := range args {
			escapedArgs[i] = strings.ReplaceAll(arg, "'", "\\'")
		}
		cmdLine += " " + strings.Join(escapedArgs, " ")
	}

	// Use osascript with administrator privileges
	script := fmt.Sprintf(`do shell script "%s" with administrator privileges`, cmdLine)
	cmd := exec.Command("osascript", "-e", script)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("failed to run elevated command: %w", err)
	}

	return string(output), nil
}

// runLinuxElevated executes command with pkexec or sudo (Linux)
func (p *PrivilegeService) runLinuxElevated(command string, args []string) (string, error) {
	// Try pkexec first (PolicyKit)
	if p.commandExists("pkexec") {
		cmdArgs := append([]string{command}, args...)
		cmd := exec.Command("pkexec", cmdArgs...)
		output, err := cmd.CombinedOutput()
		if err == nil {
			return string(output), nil
		}
		// pkexec failed, try sudo
	}

	// Fallback to sudo
	if p.commandExists("sudo") {
		cmdArgs := append([]string{"-S", command}, args...)
		cmd := exec.Command("sudo", cmdArgs...)
		output, err := cmd.CombinedOutput()
		if err != nil {
			return string(output), fmt.Errorf("failed to run elevated command with sudo: %w", err)
		}
		return string(output), nil
	}

	return "", fmt.Errorf("no privilege escalation method available (tried pkexec, sudo)")
}

// commandExists checks if a command is available in PATH
func (p *PrivilegeService) commandExists(command string) bool {
	_, err := exec.LookPath(command)
	return err == nil
}

// TestElevation tests if elevation is working
func (p *PrivilegeService) TestElevation() (bool, error) {
	var cmd string
	var args []string

	switch runtime.GOOS {
	case "windows":
		cmd = "cmd.exe"
		args = []string{"/c", "echo", "test"}
	case "darwin", "linux":
		cmd = "echo"
		args = []string{"test"}
	default:
		return false, fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}

	output, err := p.RunElevated(cmd, args)
	if err != nil {
		return false, err
	}

	return strings.Contains(strings.TrimSpace(output), "test"), nil
}

// Wails exported methods

// RunElevatedCommand runs a command with elevation (exported for Wails)
func (p *PrivilegeService) RunElevatedCommand(command string, args []string) (string, error) {
	return p.RunElevated(command, args)
}

// TestPrivilegeElevation tests elevation (exported for Wails)
func (p *PrivilegeService) TestPrivilegeElevation() (bool, error) {
	return p.TestElevation()
}
