package main

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"github.com/spf13/cobra"
)

func historyCmd() *cobra.Command {
	var startDate, endDate string

	cmd := &cobra.Command{
		Use:   "history",
		Short: "Show team history",
		Long:  `Show history for a specific team.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if projectName == "" {
				return fmt.Errorf("--project flag is required")
			}
			if teamID == 0 {
				return fmt.Errorf("--team flag is required")
			}

			historyArgs := []string{
				"--team", fmt.Sprintf("%d", teamID),
			}
			if startDate != "" {
				historyArgs = append(historyArgs, "--start-date", startDate)
			}
			if endDate != "" {
				historyArgs = append(historyArgs, "--end-date", endDate)
			}

			if output == "json" {
				historyArgs = append(historyArgs, "--format", "json")
			}

			result, err := runTeamManager(projectName, "team-history", historyArgs...)
			if err != nil {
				return err
			}

			fmt.Println(string(result))
			return nil
		},
	}

	cmd.Flags().IntVarP(&teamID, "team", "t", 0, "Team ID")
	cmd.Flags().StringVar(&startDate, "start-date", "", "Start date (ISO format)")
	cmd.Flags().StringVar(&endDate, "end-date", "", "End date (ISO format)")

	cmd.MarkFlagRequired("team")

	return cmd
}

// templateCmd creates the template command
func templateCmd() *cobra.Command {
	var format, outputFile string

	cmd := &cobra.Command{
		Use:   "template",
		Short: "Create template for bulk assignments",
		Long:  `Create a CSV or JSON template for bulk role assignments.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if projectName == "" {
				return fmt.Errorf("--project flag is required")
			}

			var cmdName string
			var fileFlag string
			switch format {
			case "csv":
				cmdName = "template-csv"
				fileFlag = "--file"
			case "json":
				cmdName = "template-json"
				fileFlag = "--file"
			default:
				return fmt.Errorf("unsupported format: %s (use csv or json)", format)
			}

			result, err := runTeamManager(projectName, cmdName, fileFlag, outputFile)
			if err != nil {
				return err
			}

			fmt.Println(string(result))
			return nil
		},
	}

	cmd.Flags().StringVarP(&format, "format", "f", "csv", "Template format (csv or json)")
	cmd.Flags().StringVarP(&outputFile, "output", "o", "", "Output file path")

	return cmd
}

// healthCmd creates the health command
func healthCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "health",
		Short: "Check team manager health status",
		Long:  `Check the health status of the team manager.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Health check doesn't require a project
			result, err := runTeamManager("", "health")
			if err != nil {
				return err
			}

			fmt.Println(string(result))
			return nil
		},
	}
}

// exportCmd creates the export command
func exportCmd() *cobra.Command {
	var format string

	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export project data",
		Long:  `Export team assignments and project data to a file.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if projectName == "" {
				return fmt.Errorf("--project flag is required")
			}

			var result []byte
			var err error

			switch format {
			case "json":
				result, err = runTeamManager(projectName, "export-json")
			case "csv":
				result, err = runTeamManager(projectName, "export-csv")
			default:
				return fmt.Errorf("unsupported export format: %s", format)
			}

			if err != nil {
				return err
			}

			// Write to stdout or file
			fmt.Println(string(result))
			return nil
		},
	}

	cmd.Flags().StringVarP(&format, "format", "f", "json", "Export format (json, csv)")
	return cmd
}

// importCmd creates the import command
func importCmd() *cobra.Command {
	var filePath string
	var format string

	cmd := &cobra.Command{
		Use:   "import",
		Short: "Import project data",
		Long:  `Import team assignments from a file.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if projectName == "" {
				return fmt.Errorf("--project flag is required")
			}
			if filePath == "" {
				return fmt.Errorf("--file flag is required")
			}

			var result []byte
			var err error

			switch format {
			case "json":
				result, err = runTeamManager(projectName, "import-json", "--file", filePath)
			case "csv":
				result, err = runTeamManager(projectName, "import-csv", "--file", filePath)
			default:
				return fmt.Errorf("unsupported import format: %s", format)
			}

			if err != nil {
				return err
			}

			fmt.Println(string(result))
			return nil
		},
	}

	cmd.Flags().StringVarP(&filePath, "file", "f", "", "Path to import file")
	cmd.Flags().StringVar(&format, "format", "json", "Import format (json, csv)")

	cmd.MarkFlagRequired("file")

	return cmd
}

// backupCmd creates the backup command
func backupCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "backup",
		Short: "List available backups",
		Long:  `List all available backups for the project.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if projectName == "" {
				return fmt.Errorf("--project flag is required")
			}

			if output == "json" {
				result, err := runTeamManager(projectName, "list-backups", "--format", "json")
				if err != nil {
					return err
				}
				fmt.Println(string(result))
				return nil
			}

			fmt.Println(titleStyle.Render("Available Backups"))
			fmt.Printf("Project: %s\n\n", textStyle.Render(projectName))

			result, err := runTeamManager(projectName, "list-backups")
			if err != nil {
				return err
			}

			fmt.Println(string(result))
			return nil
		},
	}
}

// restoreCmd creates the restore command
func restoreCmd() *cobra.Command {
	var backupFile string

	cmd := &cobra.Command{
		Use:   "restore",
		Short: "Restore from backup",
		Long:  `Restore project data from a backup file.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if projectName == "" {
				return fmt.Errorf("--project flag is required")
			}
			if backupFile == "" {
				return fmt.Errorf("--backup flag is required")
			}

			fmt.Println(titleStyle.Render("Restoring from Backup"))
			fmt.Printf("Project: %s\n", textStyle.Render(projectName))
			fmt.Printf("Backup: %s\n\n", textStyle.Render(backupFile))

			result, err := runTeamManager(projectName, "restore", "--backup", backupFile)
			if err != nil {
				return err
			}

			fmt.Println(string(result))
			return nil
		},
	}

	cmd.Flags().StringVarP(&backupFile, "backup", "b", "", "Path to backup file")
	cmd.MarkFlagRequired("backup")

	return cmd
}

// deleteCmd creates the delete command
func deleteCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "delete",
		Short: "Delete project or team",
		Long:  `Delete a project or a specific team from the project.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if projectName == "" {
				return fmt.Errorf("--project flag is required")
			}

			var result []byte
			var err error

			if teamID > 0 {
				// Delete specific team
				if !force {
					fmt.Printf("Delete team %d from project '%s'? [y/N]: ", teamID, projectName)
					var response string
					fmt.Scanln(&response)
					if !strings.EqualFold(response, "y") {
						fmt.Println("Aborted.")
						return nil
					}
				}

				fmt.Println(titleStyle.Render("Deleting Team"))
				fmt.Printf("Project: %s\n", textStyle.Render(projectName))
				fmt.Printf("Team: %d\n\n", teamID)

				result, err = runTeamManager(projectName, "delete-team", "--team", fmt.Sprintf("%d", teamID))
			} else {
				// Delete entire project
				if !force {
					fmt.Printf("Delete entire project '%s'? This cannot be undone! [y/N]: ", projectName)
					var response string
					fmt.Scanln(&response)
					if !strings.EqualFold(response, "y") {
						fmt.Println("Aborted.")
						return nil
					}
				}

				fmt.Println(titleStyle.Render("Deleting Project"))
				fmt.Printf("Project: %s\n\n", textStyle.Render(projectName))

				result, err = runTeamManager(projectName, "delete-project")
			}

			if err != nil {
				return err
			}

			fmt.Println(string(result))
			return nil
		},
	}

	cmd.Flags().IntVarP(&teamID, "team", "t", 0, "Team ID to delete (optional - if not specified, deletes entire project)")
	cmd.Flags().BoolVar(&force, "force", false, "Skip confirmation prompt")

	return cmd
}

// Helper function to pretty print JSON
func prettyPrintJSON(data []byte) error {
	var v interface{}
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	pretty, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(pretty))
	return nil
}

// CheckPython returns an error if Python is not available
func CheckPython() error {
	pythonCmd := "python3"
	if runtime.GOOS == "windows" {
		pythonCmd = "python"
	}
	_, err := exec.LookPath(pythonCmd)
	if err != nil {
		_, err = exec.LookPath("python")
		if err != nil {
			return fmt.Errorf("Python is required but not found. Please install Python 3")
		}
	}
	return nil
}
