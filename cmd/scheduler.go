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
	schedulerOnce    bool
)

var schedulerCmd = &cobra.Command{
	Use:     "scheduler",
	Aliases: []string{"cron", "schedulers"},
	Short:   "Manage scheduled (cron) and one-off jobs",
	Long: `Manage jobs that run a command in a fresh container of the service's deployed
image, with the service's variables (Heroku Scheduler / Railway Cron parity).

A job either has a cron schedule or is on demand: created with --once, it runs a
single time right away and never on its own. It stays listed, so you can run it
again with "scheduler run" or remove it with "scheduler delete".

A run is killed after --timeout seconds: 300 by default, 1800 (30 min) at most.

Examples:
  upuai scheduler list
  upuai scheduler create --name nightly --command "rails db:cleanup" --schedule "0 3 * * *"
  upuai scheduler create --name import --command "php artisan ibge:import" --once
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
			table.AddRow(j.Name, scheduleLabel(&j), j.Command, j.Status)
		}
		table.Print()
		fmt.Println()
		return nil
	},
}

// scheduleLabel é o agendamento como aparece na tabela.
func scheduleLabel(j *api.ScheduledJob) string {
	if j.OnDemand() {
		return "on demand"
	}
	return *j.Schedule
}

// validateSchedulerCreate confere as flags do `scheduler create`: nome e comando
// sempre, e exatamente um entre --schedule (cron) e --once (roda uma vez, agora).
func validateSchedulerCreate(name, command, schedule string, once bool) error {
	if name == "" || command == "" {
		return fmt.Errorf("--name and --command are required")
	}
	if schedule != "" && once {
		return fmt.Errorf("use only one of --schedule or --once")
	}
	if schedule == "" && !once {
		return fmt.Errorf("--schedule is required — or pass --once to run the command a single time, now")
	}
	return nil
}

var schedulerCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a scheduled job, or run a command once (--once)",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireAuth(); err != nil {
			return err
		}
		if err := validateSchedulerCreate(schedulerName, schedulerCommand, schedulerCron, schedulerOnce); err != nil {
			return err
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
		if schedulerOnce {
			err = ui.RunWithSpinner("Triggering run...", func() error {
				ran, e := client.RunScheduledJob(envID, serviceID, job.ID)
				if e == nil {
					job = ran
				}
				return e
			})
			if err != nil {
				// O job existe: repetir o create daria conflito de nome.
				return fmt.Errorf("job %s was created but its run did not start — retry with `upuai scheduler run %s`: %w", job.Name, shellArg(job.Name), err)
			}
		}
		if getOutputFormat() == ui.FormatJSON {
			ui.PrintJSON(job)
			return nil
		}
		if schedulerOnce {
			ui.PrintSuccess(fmt.Sprintf("Job %s created and triggered — it runs once, never on its own", job.Name))
			ui.PrintInfo(fmt.Sprintf("Run it again with `upuai scheduler run %s`; remove it with `upuai scheduler delete %s`.", shellArg(job.Name), shellArg(job.Name)))
			return nil
		}
		ui.PrintSuccess(fmt.Sprintf("Scheduled job %s created (%s)", job.Name, scheduleLabel(job)))
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
	schedulerCreateCmd.Flags().BoolVar(&schedulerOnce, "once", false, "No schedule: run the command a single time, now (the job never runs on its own)")
	schedulerCmd.AddCommand(schedulerListCmd)
	schedulerCmd.AddCommand(schedulerCreateCmd)
	schedulerCmd.AddCommand(schedulerRunCmd)
	schedulerCmd.AddCommand(schedulerPauseCmd)
	schedulerCmd.AddCommand(schedulerResumeCmd)
	schedulerCmd.AddCommand(schedulerDeleteCmd)
	rootCmd.AddCommand(schedulerCmd)
}
