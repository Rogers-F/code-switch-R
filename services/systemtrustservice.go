package services

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// SystemTrustService manages system certificate trust
type SystemTrustService struct {
	privilegeService *PrivilegeService
}

// NewSystemTrustService creates a new system trust service
func NewSystemTrustService() (*SystemTrustService, error) {
	privSvc, err := NewPrivilegeService()
	if err != nil {
		return nil, fmt.Errorf("failed to create privilege service: %w", err)
	}

	return &SystemTrustService{
		privilegeService: privSvc,
	}, nil
}

// Install installs a Root CA certificate to system trust store
func (s *SystemTrustService) Install(certPath string) error {
	var cmd string
	var args []string

	switch runtime.GOOS {
	case "windows":
		// certutil -addstore -f "ROOT" "path\to\cert.crt"
		cmd = "certutil.exe"
		args = []string{"-addstore", "-f", "ROOT", certPath}

	case "darwin":
		// security add-trusted-cert -d -r trustRoot -k /Library/Keychains/System.keychain cert.crt
		cmd = "security"
		args = []string{"add-trusted-cert", "-d", "-r", "trustRoot", "-k", "/Library/Keychains/System.keychain", certPath}

	case "linux":
		// cp cert.crt /usr/local/share/ca-certificates/code-switch-mitm.crt && update-ca-certificates
		// Note: This requires update-ca-certificates to be available (Debian/Ubuntu)
		cmd = "sh"
		args = []string{"-c", fmt.Sprintf("cp '%s' /usr/local/share/ca-certificates/code-switch-mitm.crt && update-ca-certificates", certPath)}

	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}

	// Execute with elevated privileges
	output, err := s.privilegeService.RunElevated(cmd, args)
	if err != nil {
		return fmt.Errorf("failed to install certificate: %w (output: %s)", err, output)
	}

	return nil
}

// Uninstall removes the Root CA certificate from system trust store
func (s *SystemTrustService) Uninstall(certName string) error {
	var cmd string
	var args []string

	switch runtime.GOOS {
	case "windows":
		// certutil -delstore "ROOT" "Code-Switch MITM CA"
		cmd = "certutil.exe"
		args = []string{"-delstore", "ROOT", certName}

	case "darwin":
		// security delete-certificate -c "Code-Switch MITM CA" /Library/Keychains/System.keychain
		cmd = "security"
		args = []string{"delete-certificate", "-c", certName, "/Library/Keychains/System.keychain"}

	case "linux":
		// rm -f /usr/local/share/ca-certificates/code-switch-mitm.crt && update-ca-certificates --fresh
		cmd = "sh"
		args = []string{"-c", "rm -f /usr/local/share/ca-certificates/code-switch-mitm.crt && update-ca-certificates --fresh"}

	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}

	// Execute with elevated privileges
	output, err := s.privilegeService.RunElevated(cmd, args)
	if err != nil {
		return fmt.Errorf("failed to uninstall certificate: %w (output: %s)", err, output)
	}

	return nil
}

// CheckInstalled checks if the CA certificate is installed
func (s *SystemTrustService) CheckInstalled(certName string) (bool, error) {
	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "windows":
		// certutil -store "ROOT" | findstr "Code-Switch"
		cmd = exec.Command("certutil.exe", "-store", "ROOT")

	case "darwin":
		// security find-certificate -c "Code-Switch MITM CA" /Library/Keychains/System.keychain
		cmd = exec.Command("security", "find-certificate", "-c", certName, "/Library/Keychains/System.keychain")

	case "linux":
		// Check if file exists
		cmd = exec.Command("test", "-f", "/usr/local/share/ca-certificates/code-switch-mitm.crt")

	default:
		return false, fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		// Command failed, likely not installed
		return false, nil
	}

	// For Windows and Darwin, check if certName is in output
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		return strings.Contains(string(output), certName), nil
	}

	// For Linux, test command returns 0 if file exists
	return true, nil
}

// Wails exported methods

// InstallCertificate installs a certificate (exported for Wails)
func (s *SystemTrustService) InstallCertificate(certPath string) error {
	return s.Install(certPath)
}

// UninstallCertificate uninstalls a certificate (exported for Wails)
func (s *SystemTrustService) UninstallCertificate(certName string) error {
	return s.Uninstall(certName)
}

// CheckCertificateInstalled checks if certificate is installed (exported for Wails)
func (s *SystemTrustService) CheckCertificateInstalled(certName string) (bool, error) {
	return s.CheckInstalled(certName)
}
