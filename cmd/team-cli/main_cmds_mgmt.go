package main

import (
	"encoding/json"
	"fmt"
	"github.com/charmbracelet/log"
	"github.com/spf13/cobra"
)

func validateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "validate",
		Short: "Validate team sizes meet requirements",
		Long:  `Validate that all teams have 4-6 members as required by TEAM-007.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if projectName == "" {
				return fmt.Errorf("--project flag is required")
			}

			if output == "json" {
				result, err := runTeamManager(projectName, "validate-size", "--format", "json")
				if err != nil {
					return err
				}
				fmt.Println(string(result))
				return nil
			}

			fmt.Println(titleStyle.Render("Team Size Validation"))
			fmt.Printf("Project: %s\n\n", textStyle.Render(projectName))

			result, err := runTeamManager(projectName, "validate-size")
			if err != nil {
				return err
			}

			fmt.Println(string(result))
			return nil
		},
	}
}

// phaseGateCmd creates the phase-gate command
func phaseGateCmd() *cobra.Command {
	var fromPhase, toPhase int

	cmd := &cobra.Command{
		Use:   "phase-gate",
		Short: "Check phase gate requirements",
		Long:  `Check if requirements are met for transitioning between phases.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if projectName == "" {
				return fmt.Errorf("--project flag is required")
			}
			if fromPhase == 0 || toPhase == 0 {
				return fmt.Errorf("--from and --to flags are required")
			}

			phaseGateArgs := []string{
				"--from", fmt.Sprintf("%d", fromPhase),
				"--to", fmt.Sprintf("%d", toPhase),
			}

			if output == "json" {
				phaseGateArgs = append(phaseGateArgs, "--format", "json")
				result, err := runTeamManager(projectName, "phase-gate-check", phaseGateArgs...)
				if err != nil {
					return err
				}
				fmt.Println(string(result))
				return nil
			}

			fmt.Println(titleStyle.Render("Phase Gate Check"))
			fmt.Printf("Project: %s\n", textStyle.Render(projectName))
			fmt.Printf("From: Phase %d → To: Phase %d\n\n", fromPhase, toPhase)

			result, err := runTeamManager(projectName, "phase-gate-check", phaseGateArgs...)
			if err != nil {
				return err
			}

			fmt.Println(string(result))
			return nil
		},
	}

	cmd.Flags().IntVar(&fromPhase, "from", 0, "Source phase number (1-4)")
	cmd.Flags().IntVar(&toPhase, "to", 0, "Target phase number (2-5)")

	cmd.MarkFlagRequired("from")
	cmd.MarkFlagRequired("to")

	return cmd
}

// queryCmd creates the query command
func queryCmd() *cobra.Command {
	var statusFilter, assigneeFilter, roleFilter string

	cmd := &cobra.Command{
		Use:   "query",
		Short: "Query teams with filters",
		Long:  `Query teams with filters for status, assignee, or role.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if projectName == "" {
				return fmt.Errorf("--project flag is required")
			}

			queryArgs := []string{}
			if statusFilter != "" {
				queryArgs = append(queryArgs, "--status", statusFilter)
			}
			if assigneeFilter != "" {
				queryArgs = append(queryArgs, "--assignee", assigneeFilter)
			}
			if roleFilter != "" {
				queryArgs = append(queryArgs, "--role", roleFilter)
			}

			if output == "json" {
				queryArgs = append(queryArgs, "--format", "json")
			}

			result, err := runTeamManager(projectName, "query", queryArgs...)
			if err != nil {
				return err
			}

			fmt.Println(string(result))
			return nil
		},
	}

	cmd.Flags().StringVar(&statusFilter, "status", "", "Filter by status (not_started, active, completed, blocked)")
	cmd.Flags().StringVar(&assigneeFilter, "assignee", "", "Filter by assignee")
	cmd.Flags().StringVar(&roleFilter, "role", "", "Filter by role")

	return cmd
}

// reassignCmd creates the reassign command
func reassignCmd() *cobra.Command {
	var fromTeam, toTeam int
	var fromRole, toRole, personName string

	cmd := &cobra.Command{
		Use:   "reassign",
		Short: "Reassign person from one role to another",
		Long:  `Move a person from one role/team to another role/team.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if projectName == "" {
				return fmt.Errorf("--project flag is required")
			}

			reassignArgs := []string{
				"--from-team", fmt.Sprintf("%d", fromTeam),
				"--from-role", fromRole,
				"--to-team", fmt.Sprintf("%d", toTeam),
				"--to-role", toRole,
				"--person", personName,
			}

			if output == "json" {
				reassignArgs = append(reassignArgs, "--format", "json")
			}

			result, err := runTeamManager(projectName, "reassign", reassignArgs...)
			if err != nil {
				return err
			}

			fmt.Println(string(result))
			return nil
		},
	}

	cmd.Flags().IntVar(&fromTeam, "from-team", 0, "Source team ID")
	cmd.Flags().StringVar(&fromRole, "from-role", "", "Source role")
	cmd.Flags().IntVar(&toTeam, "to-team", 0, "Target team ID")
	cmd.Flags().StringVar(&toRole, "to-role", "", "Target role")
	cmd.Flags().StringVar(&personName, "person", "", "Person to reassign")

	cmd.MarkFlagRequired("from-team")
	cmd.MarkFlagRequired("from-role")
	cmd.MarkFlagRequired("to-team")
	cmd.MarkFlagRequired("to-role")
	cmd.MarkFlagRequired("person")

	return cmd
}

// auditCmd creates the audit command
func auditCmd() *cobra.Command {
	var limit int

	cmd := &cobra.Command{
		Use:   "audit",
		Short: "Query audit log",
		Long:  `Query the audit log for project changes.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if projectName == "" {
				return fmt.Errorf("--project flag is required")
			}

			auditArgs := []string{}
			if limit > 0 {
				auditArgs = append(auditArgs, "--limit", fmt.Sprintf("%d", limit))
			}

			if output == "json" {
				auditArgs = append(auditArgs, "--format", "json")
			}

			result, err := runTeamManager(projectName, "audit", auditArgs...)
			if err != nil {
				return err
			}

			fmt.Println(string(result))
			return nil
		},
	}

	cmd.Flags().IntVar(&limit, "limit", 50, "Number of entries to show")

	return cmd
}

// historyCmd creates the history command
