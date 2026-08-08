package main

import (
	"encoding/json"
	"fmt"
	"github.com/spf13/cobra"
)

func initCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init [project-name]",
		Short: "Initialize a new project with team structure",
		Long:  `Initialize a new project with the standardized 12-team structure.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			project := args[0]

			if output == "json" {
				result, err := runTeamManager(project, "init", "--format", "json")
				if err != nil {
					return err
				}
				fmt.Println(string(result))
				return nil
			}

			fmt.Println(titleStyle.Render("Initializing Team Structure"))
			fmt.Printf("Project: %s\n\n", textStyle.Render(project))

			result, err := runTeamManager(project, "init")
			if err != nil {
				return fmt.Errorf("%s %v", errorStyle.Render("Failed to initialize project:"), err)
			}

			fmt.Println(string(result))
			fmt.Println()
			fmt.Println(successStyle.Render("✓ Project initialized successfully"))
			fmt.Println(infoStyle.Render(fmt.Sprintf("Config: .teams/%s.json", project)))

			return nil
		},
	}
}

// listCmd creates the list command
func listCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all teams for a project",
		Long:  `List all teams and their role assignments for a project.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if projectName == "" {
				return fmt.Errorf("--project flag is required")
			}

			listExtraArgs := []string{}
			if phase != "" {
				listExtraArgs = append(listExtraArgs, "--phase", phase)
			}

			if output == "json" {
				listExtraArgs = append(listExtraArgs, "--format", "json")
				result, err := runTeamManager(projectName, "list", listExtraArgs...)
				if err != nil {
					return err
				}
				fmt.Println(string(result))
				return nil
			}

			fmt.Println(titleStyle.Render("Team List"))
			fmt.Printf("Project: %s\n\n", textStyle.Render(projectName))

			result, err := runTeamManager(projectName, "list", listExtraArgs...)
			if err != nil {
				return err
			}

			fmt.Println(string(result))
			return nil
		},
	}

	cmd.Flags().StringVar(&phase, "phase", "", "Filter by phase (e.g., 'Phase 1')")
	return cmd
}

// assignCmd creates the assign command
func assignCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "assign",
		Short: "Assign a person to a role",
		Long:  `Assign a person to a specific role within a team.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if projectName == "" {
				return fmt.Errorf("--project flag is required")
			}
			if teamID == 0 {
				return fmt.Errorf("--team flag is required")
			}
			if roleName == "" {
				return fmt.Errorf("--role flag is required")
			}
			if person == "" {
				return fmt.Errorf("--person flag is required")
			}

			assignArgs := []string{
				"--team", fmt.Sprintf("%d", teamID),
				"--role", roleName,
				"--person", person,
			}

			if output == "json" {
				assignArgs = append(assignArgs, "--format", "json")
				result, err := runTeamManager(projectName, "assign", assignArgs...)
				if err != nil {
					return err
				}
				fmt.Println(string(result))
				return nil
			}

			fmt.Println(titleStyle.Render("Assigning Role"))
			fmt.Printf("Project: %s\n", textStyle.Render(projectName))
			fmt.Printf("Team: %s\n", textStyle.Render(fmt.Sprintf("Team %d", teamID)))
			fmt.Printf("Role: %s\n", textStyle.Render(roleName))
			fmt.Printf("Person: %s\n\n", textStyle.Render(person))

			result, err := runTeamManager(projectName, "assign", assignArgs...)
			if err != nil {
				return err
			}

			fmt.Println(string(result))
			return nil
		},
	}

	cmd.Flags().IntVarP(&teamID, "team", "t", 0, "Team ID (1-12)")
	cmd.Flags().StringVarP(&roleName, "role", "r", "", "Role name to assign")
	cmd.Flags().StringVar(&person, "person", "", "Person to assign")

	cmd.MarkFlagRequired("team")
	cmd.MarkFlagRequired("role")
	cmd.MarkFlagRequired("person")

	return cmd
}

// unassignCmd creates the unassign command
func unassignCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "unassign",
		Short: "Remove a person from a role",
		Long:  `Remove a person from a specific role within a team.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if projectName == "" {
				return fmt.Errorf("--project flag is required")
			}
			if teamID == 0 {
				return fmt.Errorf("--team flag is required")
			}
			if roleName == "" {
				return fmt.Errorf("--role flag is required")
			}

			unassignArgs := []string{
				"--team", fmt.Sprintf("%d", teamID),
				"--role", roleName,
			}

			if output == "json" {
				unassignArgs = append(unassignArgs, "--format", "json")
				result, err := runTeamManager(projectName, "unassign", unassignArgs...)
				if err != nil {
					return err
				}
				fmt.Println(string(result))
				return nil
			}

			fmt.Println(titleStyle.Render("Unassigning Role"))
			fmt.Printf("Project: %s\n", textStyle.Render(projectName))
			fmt.Printf("Team: %s\n", textStyle.Render(fmt.Sprintf("Team %d", teamID)))
			fmt.Printf("Role: %s\n\n", textStyle.Render(roleName))

			result, err := runTeamManager(projectName, "unassign", unassignArgs...)
			if err != nil {
				return err
			}

			fmt.Println(string(result))
			return nil
		},
	}

	cmd.Flags().IntVarP(&teamID, "team", "t", 0, "Team ID (1-12)")
	cmd.Flags().StringVarP(&roleName, "role", "r", "", "Role name to unassign")

	cmd.MarkFlagRequired("team")
	cmd.MarkFlagRequired("role")

	return cmd
}

// startCmd creates the start command
func startCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start a team",
		Long:  `Mark a team as started/in-progress.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if projectName == "" {
				return fmt.Errorf("--project flag is required")
			}
			if teamID == 0 {
				return fmt.Errorf("--team flag is required")
			}

			startArgs := []string{"--team", fmt.Sprintf("%d", teamID)}

			if output == "json" {
				startArgs = append(startArgs, "--format", "json")
				result, err := runTeamManager(projectName, "start", startArgs...)
				if err != nil {
					return err
				}
				fmt.Println(string(result))
				return nil
			}

			fmt.Println(titleStyle.Render("Starting Team"))
			fmt.Printf("Project: %s\n", textStyle.Render(projectName))
			fmt.Printf("Team: %s\n\n", textStyle.Render(fmt.Sprintf("Team %d", teamID)))

			result, err := runTeamManager(projectName, "start", startArgs...)
			if err != nil {
				return err
			}

			fmt.Println(string(result))
			return nil
		},
	}

	cmd.Flags().IntVarP(&teamID, "team", "t", 0, "Team ID (1-12)")
	cmd.MarkFlagRequired("team")

	return cmd
}

// completeCmd creates the complete command
func completeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "complete",
		Short: "Complete a team",
		Long:  `Mark a team as completed.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if projectName == "" {
				return fmt.Errorf("--project flag is required")
			}
			if teamID == 0 {
				return fmt.Errorf("--team flag is required")
			}

			completeArgs := []string{"--team", fmt.Sprintf("%d", teamID)}

			if output == "json" {
				completeArgs = append(completeArgs, "--format", "json")
				result, err := runTeamManager(projectName, "complete", completeArgs...)
				if err != nil {
					return err
				}
				fmt.Println(string(result))
				return nil
			}

			fmt.Println(titleStyle.Render("Completing Team"))
			fmt.Printf("Project: %s\n", textStyle.Render(projectName))
			fmt.Printf("Team: %s\n\n", textStyle.Render(fmt.Sprintf("Team %d", teamID)))

			result, err := runTeamManager(projectName, "complete", completeArgs...)
			if err != nil {
				return err
			}

			fmt.Println(string(result))
			return nil
		},
	}

	cmd.Flags().IntVarP(&teamID, "team", "t", 0, "Team ID (1-12)")
	cmd.MarkFlagRequired("team")

	return cmd
}

// statusCmd creates the status command
func statusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show project or phase status",
		Long:  `Display the current status of teams in a project or phase.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if projectName == "" {
				return fmt.Errorf("--project flag is required")
			}

			statusArgs := []string{}
			if phase != "" {
				statusArgs = append(statusArgs, "--phase", phase)
			}

			if output == "json" {
				statusArgs = append(statusArgs, "--format", "json")
				result, err := runTeamManager(projectName, "status", statusArgs...)
				if err != nil {
					return err
				}
				fmt.Println(string(result))
				return nil
			}

			fmt.Println(titleStyle.Render("Project Status"))
			fmt.Printf("Project: %s\n\n", textStyle.Render(projectName))

			result, err := runTeamManager(projectName, "status", statusArgs...)
			if err != nil {
				return err
			}

			fmt.Println(string(result))
			return nil
		},
	}

	cmd.Flags().StringVar(&phase, "phase", "", "Show status for specific phase")
	return cmd
}

// validateCmd creates the validate command
