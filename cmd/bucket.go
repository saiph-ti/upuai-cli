package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/upuai-cloud/cli/internal/api"
	"github.com/upuai-cloud/cli/internal/ui"
)

// `upuai bucket ...` is the public-facing surface for bucket-level operations.
// Today only `public` is exposed — toggles anonymous read on the bucket via
// the canonical platform path. Runbook:
// upuai-core/docs/runbooks/2026-05-05-public-bucket-access.md

var bucketTargetFlag string

var bucketCmd = &cobra.Command{
	Use:     "bucket",
	Aliases: []string{"buckets"},
	Short:   "Manage buckets in the linked project",
	Long: `Manage buckets in the linked project.

Examples:
  upuai bucket public                  Show public access state and URL
  upuai bucket public enable           Enable anonymous read (with confirm)
  upuai bucket public enable --yes     Enable anonymous read (CI/non-interactive)
  upuai bucket public disable          Disable anonymous read

By default the bucket is auto-selected when the linked project has exactly one.
With multiple buckets, pass --bucket <name> to disambiguate.`,
}

var bucketPublicCmd = &cobra.Command{
	Use:   "public",
	Short: "Inspect bucket public access (anonymous read)",
	Long: `Print whether anonymous read is enabled on the bucket and the canonical public URL.

Use 'enable' or 'disable' subcommands to toggle.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		bucket, err := resolveBucket()
		if err != nil {
			return err
		}
		client := api.NewClient()
		var info *api.BucketPublicAccessInfo
		if err := ui.RunWithSpinner("Reading public access state...", func() error {
			var apiErr error
			info, apiErr = client.GetBucketPublicAccess(bucket.ID)
			return apiErr
		}); err != nil {
			return fmt.Errorf("get public access: %w", err)
		}
		printBucketPublicAccess(bucket, info)
		return nil
	},
}

var bucketPublicEnableCmd = &cobra.Command{
	Use:   "enable",
	Short: "Enable anonymous read on the bucket",
	Long: `Enable anonymous read on the bucket. Applies the canonical MinIO bucket
policy (s3:GetObject, scoped to the bucket) plus permissive CORS for browser
fetches. Listing and version history remain blocked.

Anyone with the public URL can download objects after this — confirms required
unless --yes is set.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		bucket, err := resolveBucket()
		if err != nil {
			return err
		}
		if !flagYes {
			ui.PrintWarning(fmt.Sprintf(
				"This will make all objects in bucket %q readable by anyone with the URL.",
				bucket.Name,
			))
			confirmed, err := ui.Confirm("Continue?")
			if err != nil {
				return err
			}
			if !confirmed {
				ui.PrintInfo("aborted")
				return nil
			}
		}
		client := api.NewClient()
		var info *api.BucketPublicAccessInfo
		if err := ui.RunWithSpinner("Enabling public access...", func() error {
			var apiErr error
			info, apiErr = client.SetBucketPublicAccess(bucket.ID, true)
			return apiErr
		}); err != nil {
			return mapPublicAccessError(err)
		}
		ui.PrintSuccess("public access enabled")
		printBucketPublicAccess(bucket, info)
		return nil
	},
}

var bucketPublicDisableCmd = &cobra.Command{
	Use:   "disable",
	Short: "Disable anonymous read on the bucket",
	RunE: func(cmd *cobra.Command, args []string) error {
		bucket, err := resolveBucket()
		if err != nil {
			return err
		}
		client := api.NewClient()
		var info *api.BucketPublicAccessInfo
		if err := ui.RunWithSpinner("Disabling public access...", func() error {
			var apiErr error
			info, apiErr = client.SetBucketPublicAccess(bucket.ID, false)
			return apiErr
		}); err != nil {
			return mapPublicAccessError(err)
		}
		ui.PrintSuccess("public access disabled")
		printBucketPublicAccess(bucket, info)
		return nil
	},
}

// resolveBucket picks the target bucket from --bucket/-b or auto-selects the
// project's only bucket. Returns a clear error when the user must disambiguate.
func resolveBucket() (*api.Bucket, error) {
	if err := requireAuth(); err != nil {
		return nil, err
	}
	projectID, err := requireProject()
	if err != nil {
		return nil, err
	}
	client := api.NewClient()
	buckets, err := client.ListProjectBuckets(projectID)
	if err != nil {
		return nil, fmt.Errorf("list project buckets: %w", err)
	}
	if len(buckets) == 0 {
		return nil, fmt.Errorf("no buckets in this project — create one with `upuai add` (type: bucket)")
	}
	if bucketTargetFlag != "" {
		for i := range buckets {
			if buckets[i].Name == bucketTargetFlag || buckets[i].ID == bucketTargetFlag {
				return &buckets[i], nil
			}
		}
		return nil, fmt.Errorf("bucket %q not found in this project", bucketTargetFlag)
	}
	if len(buckets) > 1 {
		names := make([]string, len(buckets))
		for i, b := range buckets {
			names[i] = b.Name
		}
		return nil, fmt.Errorf(
			"this project has %d buckets (%v) — pass --bucket <name> to choose one",
			len(buckets), names,
		)
	}
	return &buckets[0], nil
}

func printBucketPublicAccess(bucket *api.Bucket, info *api.BucketPublicAccessInfo) {
	if getOutputFormat() == ui.FormatJSON {
		ui.PrintJSON(info)
		return
	}
	state := "disabled"
	if info.Enabled {
		state = "enabled"
	}
	ui.PrintKeyValue(
		"Bucket", bucket.Name,
		"Public access", state,
		"Public URL", info.PublicURL,
	)
}

// mapPublicAccessError turns API status codes into actionable copy. The role
// gate (403) and versioning conflict (412) are the two cases users hit most
// often — a generic "request failed" leaves them guessing.
func mapPublicAccessError(err error) error {
	if apiErr, ok := err.(*api.APIError); ok {
		switch apiErr.StatusCode {
		case 403:
			return fmt.Errorf(
				"forbidden: changing bucket visibility requires OWNER or ADMIN role on this tenant",
			)
		case 412:
			return fmt.Errorf(
				"bucket has versioning enabled — suspend versioning before enabling public access",
			)
		}
	}
	return fmt.Errorf("public access toggle failed: %w", err)
}

func init() {
	bucketPublicCmd.AddCommand(bucketPublicEnableCmd)
	bucketPublicCmd.AddCommand(bucketPublicDisableCmd)
	bucketCmd.AddCommand(bucketPublicCmd)
	bucketCmd.PersistentFlags().StringVarP(
		&bucketTargetFlag, "bucket", "b", "",
		"Bucket name (or ID) — required when project has multiple buckets",
	)
	rootCmd.AddCommand(bucketCmd)
}
