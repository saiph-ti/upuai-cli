package cmd

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/upuai-cloud/cli/internal/api"
	"github.com/upuai-cloud/cli/internal/config"
	"github.com/upuai-cloud/cli/internal/ui"
)

var catalogCmd = &cobra.Command{
	Use:     "catalog",
	Aliases: []string{"templates"},
	Short:   "Browse and deploy stack templates (WordPress, etc.)",
	Long: `Stack templates provision a complete multi-service app (e.g. WordPress =
wordpress container + MySQL + persistent volume) with one command.

Examples:
  upuai catalog list
  upuai catalog describe wordpress
  upuai catalog deploy wordpress --name my-blog --input adminEmail=me@example.com`,
}

var (
	flagCatalogCategory string
	flagCatalogTag      string
)

var catalogListCmd = &cobra.Command{
	Use:   "list",
	Short: "List available stack templates",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireAuth(); err != nil {
			return err
		}

		client := api.NewClient()
		var templates []api.StackTemplate
		err := ui.RunWithSpinner("Loading catalog...", func() error {
			var fetchErr error
			templates, fetchErr = client.ListStackTemplates(flagCatalogCategory, flagCatalogTag)
			return fetchErr
		})
		if err != nil {
			return fmt.Errorf("failed to list catalog: %w", err)
		}

		format := getOutputFormat()
		if format == ui.FormatJSON {
			ui.PrintJSON(templates)
			return nil
		}

		if len(templates) == 0 {
			ui.PrintInfo("No templates available")
			return nil
		}

		fmt.Println()
		table := ui.NewTable("Slug", "Version", "Name", "Category", "Tags")
		for _, t := range templates {
			table.AddRow(t.Slug, t.Version, t.DisplayName, t.Category, strings.Join(t.Tags, ", "))
		}
		table.Print()
		fmt.Println()
		return nil
	},
}

var catalogDescribeCmd = &cobra.Command{
	Use:   "describe <slug>",
	Short: "Show inputs and metadata of a stack template",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireAuth(); err != nil {
			return err
		}
		slug := args[0]

		client := api.NewClient()
		var t *api.StackTemplate
		err := ui.RunWithSpinner("Loading template...", func() error {
			var fetchErr error
			t, fetchErr = client.GetStackTemplate(slug, "")
			return fetchErr
		})
		if err != nil {
			return fmt.Errorf("failed to get template: %w", err)
		}

		format := getOutputFormat()
		if format == ui.FormatJSON {
			ui.PrintJSON(t)
			return nil
		}

		fmt.Println()
		desc := ""
		if t.Description != nil {
			desc = *t.Description
		}
		docs := ""
		if t.DocsURL != nil {
			docs = *t.DocsURL
		}
		ui.PrintKeyValue(
			"Slug", t.Slug,
			"Version", t.Version,
			"Name", t.DisplayName,
			"Category", t.Category,
			"Tags", strings.Join(t.Tags, ", "),
			"Description", desc,
			"Docs", docs,
		)

		required := make(map[string]bool, len(t.InputsSchema.Required))
		for _, k := range t.InputsSchema.Required {
			required[k] = true
		}

		if len(t.InputsSchema.Properties) > 0 {
			fmt.Println()
			fmt.Println(ui.Bold.Render("Inputs:"))
			fmt.Println()
			table := ui.NewTable("Field", "Type", "Required", "Default", "Constraints")
			for key, prop := range t.InputsSchema.Properties {
				constraints := []string{}
				if len(prop.Enum) > 0 {
					enumVals := make([]string, len(prop.Enum))
					for i, v := range prop.Enum {
						enumVals[i] = fmt.Sprintf("%v", v)
					}
					constraints = append(constraints, "enum: "+strings.Join(enumVals, "|"))
				}
				if prop.Format != "" {
					constraints = append(constraints, "format: "+prop.Format)
				}
				if prop.Pattern != "" {
					constraints = append(constraints, "pattern: "+prop.Pattern)
				}
				if prop.MinLength != nil {
					constraints = append(constraints, fmt.Sprintf("minLen: %d", *prop.MinLength))
				}
				if prop.MaxLength != nil {
					constraints = append(constraints, fmt.Sprintf("maxLen: %d", *prop.MaxLength))
				}
				def := ""
				if prop.Default != nil {
					def = fmt.Sprintf("%v", prop.Default)
				}
				reqStr := ""
				if required[key] {
					reqStr = "yes"
				}
				table.AddRow(key, prop.Type, reqStr, def, strings.Join(constraints, ", "))
			}
			table.Print()
			fmt.Println()
		}
		return nil
	},
}

var (
	flagCatalogDeployName    string
	flagCatalogDeployVersion string
	flagCatalogDeployEnvID   string
	flagCatalogDeployInputs  []string
	flagCatalogDeployYes     bool
	flagCatalogDeployNoWait  bool
)

const (
	stackPollInterval = 3 * time.Second
	stackPollTimeout  = 5 * time.Minute
)

var catalogDeployCmd = &cobra.Command{
	Use:   "deploy <slug>",
	Short: "Deploy a stack template",
	Long: `Provisions a new stack instance from a template. Interactive by default:
prompts for each input defined in the template's schema. Use --input key=value
(repeatable) and --yes for non-interactive CI use.

Examples:
  upuai catalog deploy wordpress
  upuai catalog deploy wordpress --name my-blog \
    --input siteTitle="Meu blog" \
    --input adminEmail=admin@example.com --yes`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireAuth(); err != nil {
			return err
		}
		slug := args[0]

		projectID, err := requireProject()
		if err != nil {
			return err
		}

		client := api.NewClient()

		// 1. Fetch template to learn inputsSchema
		var t *api.StackTemplate
		err = ui.RunWithSpinner("Loading template...", func() error {
			var fetchErr error
			t, fetchErr = client.GetStackTemplate(slug, flagCatalogDeployVersion)
			return fetchErr
		})
		if err != nil {
			return fmt.Errorf("failed to load template %q: %w", slug, err)
		}

		// 2. Resolve environment — flag > project config
		environmentID := flagCatalogDeployEnvID
		if environmentID == "" {
			cfg, _ := config.LoadProjectConfig()
			if cfg != nil {
				environmentID = cfg.EnvironmentID
			}
		}
		if environmentID == "" {
			// interactive fallback
			envs, envErr := client.ListEnvironments(projectID)
			if envErr != nil {
				return fmt.Errorf("failed to list environments: %w", envErr)
			}
			if len(envs) == 0 {
				return fmt.Errorf("no environments found in project — create one first with 'upuai env new <name>'")
			}
			if flagCatalogDeployYes {
				environmentID = envs[0].ID
			} else {
				names := make([]string, len(envs))
				for i, e := range envs {
					names[i] = e.Name
				}
				selected, selErr := ui.SelectOne("Environment:", names)
				if selErr != nil {
					return selErr
				}
				for i, n := range names {
					if n == selected {
						environmentID = envs[i].ID
						break
					}
				}
			}
		}

		// 3. Resolve stack name — flag > derived
		stackName := flagCatalogDeployName
		if stackName == "" {
			if flagCatalogDeployYes {
				stackName = fmt.Sprintf("%s-%d", slug, time.Now().Unix()%10000)
			} else {
				suggested := fmt.Sprintf("%s-%d", slug, time.Now().Unix()%10000)
				val, inErr := ui.InputText("Stack name", suggested)
				if inErr != nil {
					return inErr
				}
				if strings.TrimSpace(val) == "" {
					val = suggested
				}
				stackName = val
			}
		}

		// 4. Resolve inputs — parse --input key=value, prompt for missing required
		inputs, err := resolveStackInputs(t.InputsSchema, flagCatalogDeployInputs, flagCatalogDeployYes)
		if err != nil {
			return err
		}

		// 5. Confirm (interactive only)
		if !flagCatalogDeployYes {
			fmt.Println()
			ui.PrintKeyValue(
				"Template", fmt.Sprintf("%s v%s", t.Slug, t.Version),
				"Name", stackName,
				"Environment", environmentID,
			)
			fmt.Println()
			ok, confirmErr := ui.Confirm("Deploy stack?")
			if confirmErr != nil {
				return confirmErr
			}
			if !ok {
				return nil
			}
		}

		// 6. POST deploy
		var resp *api.DeployStackResponse
		err = ui.RunWithSpinner("Deploying stack...", func() error {
			var deployErr error
			resp, deployErr = client.DeployStack(projectID, &api.DeployStackRequest{
				TemplateSlug:    t.Slug,
				TemplateVersion: t.Version,
				EnvironmentID:   environmentID,
				Name:            stackName,
				Inputs:          inputs,
			})
			return deployErr
		})
		if err != nil {
			return fmt.Errorf("failed to deploy stack: %w", err)
		}

		// 7. Poll until terminal status (unless --no-wait)
		finalStatus := resp.Status
		finalOutputs := resp.Outputs
		finalFailure := resp.FailureReason

		if !flagCatalogDeployNoWait && !isTerminalStackStatus(finalStatus) {
			pollErr := ui.RunWithSpinner("Waiting for stack to come up...", func() error {
				deadline := time.Now().Add(stackPollTimeout)
				for time.Now().Before(deadline) {
					inst, err := client.GetStackInstance(projectID, resp.StackID)
					if err != nil {
						return err
					}
					finalStatus = inst.Status
					finalOutputs = inst.Outputs
					if inst.FailureReason != nil {
						finalFailure = *inst.FailureReason
					}
					if isTerminalStackStatus(finalStatus) {
						return nil
					}
					time.Sleep(stackPollInterval)
				}
				return fmt.Errorf("timed out after %s", stackPollTimeout)
			})
			if pollErr != nil {
				ui.PrintWarning(fmt.Sprintf("Polling error: %v", pollErr))
			}
		}

		format := getOutputFormat()
		if format == ui.FormatJSON {
			ui.PrintJSON(map[string]interface{}{
				"stackId":       resp.StackID,
				"status":        finalStatus,
				"outputs":       finalOutputs,
				"failureReason": finalFailure,
			})
			return nil
		}

		fmt.Println()
		if finalStatus == "RUNNING" {
			ui.PrintSuccess(fmt.Sprintf("Stack %s is running", stackName))
		} else if finalStatus == "PARTIAL" || finalStatus == "FAILED" {
			ui.PrintError(fmt.Sprintf("Stack %s ended as %s", stackName, finalStatus))
			if finalFailure != "" {
				ui.PrintInfo(finalFailure)
			}
		} else {
			ui.PrintInfo(fmt.Sprintf("Stack %s is %s (still provisioning)", stackName, finalStatus))
		}
		ui.PrintKeyValue("Stack ID", resp.StackID, "Status", finalStatus)
		if len(finalOutputs) > 0 {
			fmt.Println()
			fmt.Println(ui.Bold.Render("Outputs:"))
			for k, v := range finalOutputs {
				fmt.Printf("  %s = %s\n", k, v)
			}
		}
		fmt.Println()
		ui.PrintInfo(fmt.Sprintf("Run 'upuai stack get %s' to check status", resp.StackID))

		return nil
	},
}

func isTerminalStackStatus(s string) bool {
	switch s {
	case "RUNNING", "PARTIAL", "FAILED", "DELETED":
		return true
	}
	return false
}

// resolveStackInputs merges --input flags with interactive prompts for missing
// required fields. Respects defaults. Validates against inputsSchema server-side
// fallback — here we only do shape validation (type coercion + required check).
func resolveStackInputs(schema api.StackTemplateInputSchema, flagInputs []string, yes bool) (map[string]interface{}, error) {
	provided := map[string]interface{}{}

	for _, kv := range flagInputs {
		idx := strings.Index(kv, "=")
		if idx <= 0 {
			return nil, fmt.Errorf("invalid --input %q (expected key=value)", kv)
		}
		key := strings.TrimSpace(kv[:idx])
		rawVal := kv[idx+1:]
		prop, ok := schema.Properties[key]
		if !ok {
			return nil, fmt.Errorf("unknown input %q (not in template schema)", key)
		}
		coerced, err := coerceInputValue(rawVal, prop)
		if err != nil {
			return nil, fmt.Errorf("input %q: %w", key, err)
		}
		provided[key] = coerced
	}

	// Apply defaults for anything not provided
	for key, prop := range schema.Properties {
		if _, set := provided[key]; set {
			continue
		}
		if prop.Default != nil {
			provided[key] = prop.Default
		}
	}

	// Check required; prompt interactively if missing
	required := schema.Required
	for _, key := range required {
		if _, set := provided[key]; set {
			continue
		}
		prop := schema.Properties[key]
		if yes {
			return nil, fmt.Errorf("required input %q missing (use --input %s=<value>)", key, key)
		}
		val, err := promptForInput(key, prop)
		if err != nil {
			return nil, err
		}
		provided[key] = val
	}

	return provided, nil
}

func coerceInputValue(raw string, prop api.StackTemplateInputProperty) (interface{}, error) {
	switch prop.Type {
	case "integer":
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("expected integer, got %q", raw)
		}
		return n, nil
	case "number":
		f, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return nil, fmt.Errorf("expected number, got %q", raw)
		}
		return f, nil
	case "boolean":
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return nil, fmt.Errorf("expected true/false, got %q", raw)
		}
		return b, nil
	default:
		return raw, nil
	}
}

func promptForInput(key string, prop api.StackTemplateInputProperty) (interface{}, error) {
	if len(prop.Enum) > 0 {
		choices := make([]string, len(prop.Enum))
		for i, v := range prop.Enum {
			choices[i] = fmt.Sprintf("%v", v)
		}
		selected, err := ui.SelectOne(key+":", choices)
		if err != nil {
			return nil, err
		}
		return selected, nil
	}
	placeholder := ""
	if prop.Default != nil {
		placeholder = fmt.Sprintf("%v", prop.Default)
	}
	val, err := ui.InputText(key, placeholder)
	if err != nil {
		return nil, err
	}
	if val == "" && prop.Default != nil {
		return prop.Default, nil
	}
	return coerceInputValue(val, prop)
}

func init() {
	catalogListCmd.Flags().StringVar(&flagCatalogCategory, "category", "", "Filter by category (cms, database, etc.)")
	catalogListCmd.Flags().StringVar(&flagCatalogTag, "tag", "", "Filter by tag")

	catalogDeployCmd.Flags().StringVar(&flagCatalogDeployName, "name", "", "Stack name (lowercase letters, digits, hyphens)")
	catalogDeployCmd.Flags().StringVar(&flagCatalogDeployVersion, "version", "", "Template version (default: active/latest)")
	catalogDeployCmd.Flags().StringVar(&flagCatalogDeployEnvID, "env-id", "", "Environment ID (overrides project config)")
	catalogDeployCmd.Flags().StringArrayVar(&flagCatalogDeployInputs, "input", nil, "Input key=value (repeatable)")
	catalogDeployCmd.Flags().BoolVarP(&flagCatalogDeployYes, "yes", "y", false, "Non-interactive: skip prompts and confirmation")
	catalogDeployCmd.Flags().BoolVar(&flagCatalogDeployNoWait, "no-wait", false, "Return after POST /deploy without polling status")

	catalogCmd.AddCommand(catalogListCmd)
	catalogCmd.AddCommand(catalogDescribeCmd)
	catalogCmd.AddCommand(catalogDeployCmd)
	rootCmd.AddCommand(catalogCmd)
}
