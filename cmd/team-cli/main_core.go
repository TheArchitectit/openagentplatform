package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/log"
	"github.com/spf13/cobra"
)

var (
	// Version info - set by ldflags during build
	version   = "dev"
	buildTime = "unknown"
	gitCommit = "unknown"

	// CLI flags
	projectName string
	phase       string
	teamID      int
	roleName    string
	person      string
	output      string

	// Styles
	titleStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7C3AED"))
	textStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#E5E7EB"))
	successStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#10B981"))
	errorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#EF4444"))
	warnStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#F59E0B"))
	infoStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#3B82F6"))
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "team",
		Short: "Team Manager CLI - Manage standardized team layouts",
		Long: `Team Manager CLI

A command-line tool for managing standardized team structures across projects.
Integrates with the team_manager.py backend to provide team initialization,
role assignments, and status tracking.`,
		Version: fmt.Sprintf("%s (built: %s, commit: %s)", version, buildTime, gitCommit),
	}

	// Global flags
	rootCmd.PersistentFlags().StringVarP(&projectName, "project", "p", "", "Project name (required for most commands)")
	rootCmd.PersistentFlags().StringVarP(&output, "output", "o", "text", "Output format: text, json")

	// Add subcommands
	rootCmd.AddCommand(initCmd())
	rootCmd.AddCommand(listCmd())
	rootCmd.AddCommand(assignCmd())
	rootCmd.AddCommand(unassignCmd())
	rootCmd.AddCommand(startCmd())
	rootCmd.AddCommand(completeCmd())
	rootCmd.AddCommand(statusCmd())
	rootCmd.AddCommand(validateCmd())
	rootCmd.AddCommand(phaseGateCmd())
	rootCmd.AddCommand(queryCmd())
	rootCmd.AddCommand(reassignCmd())
	rootCmd.AddCommand(exportCmd())
	rootCmd.AddCommand(importCmd())
	rootCmd.AddCommand(backupCmd())
	rootCmd.AddCommand(restoreCmd())
	rootCmd.AddCommand(deleteCmd())
	rootCmd.AddCommand(auditCmd())
	rootCmd.AddCommand(historyCmd())
	rootCmd.AddCommand(templateCmd())
	rootCmd.AddCommand(healthCmd())

	if err := rootCmd.Execute(); err != nil {
		log.Error(err)
		os.Exit(1)
	}
}

// getTeamManagerPath returns the path to the team_manager.py script
func getTeamManagerPath() string {
	// Check if TEAM_MANAGER_PATH env var is set
	if path := os.Getenv("TEAM_MANAGER_PATH"); path != "" {
		return path
	}

	// Try to find relative to executable
	exe, err := os.Executable()
	if err == nil {
		dir := filepath.Dir(exe)
		// Try multiple relative paths
		candidates := []string{
			filepath.Join(dir, "..", "..", "..", "scripts", "team_manager.py"),
			filepath.Join(dir, "..", "..", "scripts", "team_manager.py"),
			filepath.Join(dir, "..", "scripts", "team_manager.py"),
			filepath.Join(dir, "scripts", "team_manager.py"),
		}
		for _, candidate := range candidates {
			if _, err := os.Stat(candidate); err == nil {
				return candidate
			}
		}
	}

	// Default path
	return "scripts/team_manager.py"
}

// runTeamManager executes the team_manager.py script with the given arguments
// The Python script expects: --project PROJECT COMMAND [args...]
func runTeamManager(project string, command string, args ...string) ([]byte, error) {
	scriptPath := getTeamManagerPath()

	// Check if Python is available
	pythonCmd := "python3"
	if _, err := exec.LookPath("python3"); err != nil {
		pythonCmd = "python"
		if _, err := exec.LookPath("python"); err != nil {
			return nil, fmt.Errorf("Python not found. Please install Python 3")
		}
	}

	// Build command: script --project PROJECT command [args...]
	cmdArgs := []string{scriptPath}
	// Always include --project since Python script requires it
	if project == "" {
		project = "_cli_health_check_"
	}
	cmdArgs = append(cmdArgs, "--project", project)
	cmdArgs = append(cmdArgs, command)
	cmdArgs = append(cmdArgs, args...)

	cmd := exec.Command(pythonCmd, cmdArgs...)
	cmd.Stderr = os.Stderr

	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("team_manager.py failed: %s", string(exitErr.Stderr))
		}
		return nil, fmt.Errorf("failed to run team_manager.py: %w", err)
	}

	return output, nil
}

// initCmd creates the init command
