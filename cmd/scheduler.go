package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/upuai-cloud/cli/internal/api"
	"github.com/upuai-cloud/cli/internal/ui"
)

var (
	schedulerService string
	schedulerName    string
	schedulerCommand string
	schedulerCron    string
	schedulerTimeout int
)

var schedulerCmd = &cobra.Command{
	Use:     "scheduler",
	Aliases: []string{"cron", "schedulers"},
	Short:   "Manage scheduled (cron) jobs",
	Long: `Manage scheduled jobs that run a command on a cron schedule using the
service's deployed image (Heroku Scheduler / Railway Cron parity).

Examples:
  upuai scheduler list
  upuai scheduler create --name nightly --command "rails db:cleanup" --schedule "0 3 * * *"
  upuai scheduler run nightly
  upuai scheduler pause nightly
  upuai scheduler resume nightly
  upuai scheduler delete nightly`,
}

// resolveScheduledJob resolve um job por id OU name (case-insensitive).
func resolveScheduledJob(client *api.Client, envID, serviceID, ref string) (*api.ScheduledJob, error) {
	jobs, err := client.ListScheduledJobs(envID, serviceID)
	if err != nil {
		return nil, err
	}
	for i := range jobs {
		if jobs[i].ID == ref || strings.EqualFold(jobs[i].Name, ref) {
			return &jobs[i], nil
		}
	}
	return nil, fmt.Errorf("scheduled job %q not found", ref)
}

var schedulerListCmd = &cobra.Command{
	Use:   "list",
	Short: "List scheduled jobs",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireAuth(); err != nil {
			return err
		}
		envID, serviceID, err := resolveServiceContext(schedulerService)
		if err != nil {
			return err
		}
		client := api.NewClient()
		var jobs []api.ScheduledJob
		err = ui.RunWithSpinner("Loading scheduled jobs...", func() error {
			var e error
			jobs, e = client.ListScheduledJobs(envID, serviceID)
			return e
		})
		if err != nil {
			return fmt.Errorf("failed to list scheduled jobs: %w", err)
		}
		if getOutputFormat() == ui.FormatJSON {
			ui.PrintJSON(jobs)
			return nil
		}
		if len(jobs) == 0 {
			ui.PrintInfo("No scheduled jobs")
			return nil
		}
		fmt.Println()
		table := ui.NewTable("Name", "Schedule", "Command", "Status")
		for _, j := range jobs {
			table.AddRow(j.Name, j.Schedule, j.Command, j.Status)
		}
		table.Print()
		fmt.Println()
		return nil
	},
}

var schedulerCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a scheduled job",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireAuth(); err != nil {
			return err
		}
		if schedulerName == "" || schedulerCommand == "" || schedulerCron == "" {
			return fmt.Errorf("--name, --command and --schedule are required")
		}
		envID, serviceID, err := resolveServiceContext(schedulerService)
		if err != nil {
			return err
		}
		client := api.NewClient()
		var job *api.ScheduledJob
		err = ui.RunWithSpinner("Creating scheduled job...", func() error {
			var e error
			job, e = client.CreateScheduledJob(envID, serviceID, &api.CreateScheduledJobRequest{
				Name:           schedulerName,
				Command:        schedulerCommand,
				Schedule:       schedulerCron,
				TimeoutSeconds: schedulerTimeout,
			})
			return e
		})
		if err != nil {
			return fmt.Errorf("failed to create scheduled job: %w", err)
		}
		if getOutputFormat() == ui.FormatJSON {
			ui.PrintJSON(job)
			return nil
		}
		ui.PrintSuccess(fmt.Sprintf("Scheduled job %s created (%s)", job.Name, job.Schedule))
		return nil
	},
}

var schedulerRunCmd = &cobra.Command{
	Use:   "run <name|id>",
	Short: "Run a scheduled job now (one-off)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireAuth(); err != nil {
			return err
		}
		envID, serviceID, err := resolveServiceContext(schedulerService)
		if err != nil {
			return err
		}
		client := api.NewClient()
		job, err := resolveScheduledJob(client, envID, serviceID, args[0])
		if err != nil {
			return err
		}
		err = ui.RunWithSpinner("Triggering run...", func() error {
			_, e := client.RunScheduledJob(envID, serviceID, job.ID)
			return e
		})
		if err != nil {
			return fmt.Errorf("failed to run scheduled job: %w", err)
		}
		ui.PrintSuccess(fmt.Sprintf("Triggered %s", job.Name))
		return nil
	},
}

func setSchedulerStatus(ref, status, verb string) error {
	if err := requireAuth(); err != nil {
		return err
	}
	envID, serviceID, err := resolveServiceContext(schedulerService)
	if err != nil {
		return err
	}
	client := api.NewClient()
	job, err := resolveScheduledJob(client, envID, serviceID, ref)
	if err != nil {
		return err
	}
	err = ui.RunWithSpinner(verb+"...", func() error {
		_, e := client.UpdateScheduledJob(envID, serviceID, job.ID, &api.UpdateScheduledJobRequest{Status: status})
		return e
	})
	if err != nil {
		return fmt.Errorf("failed to %s scheduled job: %w", strings.ToLower(verb), err)
	}
	ui.PrintSuccess(fmt.Sprintf("%s %s", verb, job.Name))
	return nil
}

var schedulerPauseCmd = &cobra.Command{
	Use:   "pause <name|id>",
	Short: "Pause a scheduled job",
	Args:  cobra.ExactArgs(1),
	RunE:  func(cmd *cobra.Command, args []string) error { return setSchedulerStatus(args[0], "PAUSED", "Paused") },
}

var schedulerResumeCmd = &cobra.Command{
	Use:   "resume <name|id>",
	Short: "Resume a paused scheduled job",
	Args:  cobra.ExactArgs(1),
	RunE:  func(cmd *cobra.Command, args []string) error { return setSchedulerStatus(args[0], "ACTIVE", "Resumed") },
}

var schedulerDeleteCmd = &cobra.Command{
	Use:   "delete <name|id>",
	Short: "Delete a scheduled job",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireAuth(); err != nil {
			return err
		}
		envID, serviceID, err := resolveServiceContext(schedulerService)
		if err != nil {
			return err
		}
		client := api.NewClient()
		job, err := resolveScheduledJob(client, envID, serviceID, args[0])
		if err != nil {
			return err
		}
		if !flagYes {
			confirmed, err := ui.Confirm(fmt.Sprintf("Delete scheduled job %q?", job.Name))
			if err != nil {
				return err
			}
			if !confirmed {
				ui.PrintInfo("Delete cancelled")
				return nil
			}
		}
		err = ui.RunWithSpinner("Deleting scheduled job...", func() error {
			return client.DeleteScheduledJob(envID, serviceID, job.ID)
		})
		if err != nil {
			return fmt.Errorf("failed to delete scheduled job: %w", err)
		}
		ui.PrintSuccess(fmt.Sprintf("Deleted %s", job.Name))
		return nil
	},
}

func init() {
	schedulerCmd.PersistentFlags().StringVarP(&schedulerService, "service", "s", "", "Service name, slug, or ID (overrides linked service)")
	schedulerCreateCmd.Flags().StringVar(&schedulerName, "name", "", "Scheduled job name (lowercase, hyphens)")
	schedulerCreateCmd.Flags().StringVar(&schedulerCommand, "command", "", "Command to run")
	schedulerCreateCmd.Flags().StringVar(&schedulerCron, "schedule", "", "Cron expression (e.g. \"0 3 * * *\") or @shortcut")
	schedulerCreateCmd.Flags().IntVar(&schedulerTimeout, "timeout", 0, "Max run duration in seconds (10-1800, default 300)")
	schedulerCmd.AddCommand(schedulerListCmd)
	schedulerCmd.AddCommand(schedulerCreateCmd)
	schedulerCmd.AddCommand(schedulerRunCmd)
	schedulerCmd.AddCommand(schedulerPauseCmd)
	schedulerCmd.AddCommand(schedulerResumeCmd)
	schedulerCmd.AddCommand(schedulerDeleteCmd)
	rootCmd.AddCommand(schedulerCmd)
}
