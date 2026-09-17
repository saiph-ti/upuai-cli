package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/upuai-cloud/cli/internal/api"
	"github.com/upuai-cloud/cli/internal/ui"
)

// `upuai volume ...` gerencia o disco persistente de um serviço. Um volume é
// ReadWriteOnce: monta num pod por vez. Por isso o serviço com volume roda uma
// réplica só e cada deploy para o pod antigo antes de subir o novo — as duas
// consequências aparecem no texto dos comandos, não só na doc.

var (
	volumeServiceRef string
	volumeMountPath  string
	volumeSizeGb     int
	volumeName       string
)

const mbPerGb = 1024

var volumeCmd = &cobra.Command{
	Use:     "volume",
	Aliases: []string{"volumes"},
	Short:   "Manage persistent disks of the linked project",
	Long: `Manage persistent disks (volumes) of the linked project.

Examples:
  upuai volume list                          Volumes of the project and where they are mounted
  upuai volume add --path /data --size 5     Create a 5 GB disk and mount it on the service
  upuai volume remove <name|id> --yes        Detach and delete the disk (data is lost)

A volume is ReadWriteOnce: it attaches to ONE pod at a time. A service with a
volume therefore runs a single replica, and each deploy stops the old pod before
starting the new one (a few seconds of downtime).`,
}

var volumeListCmd = &cobra.Command{
	Use:   "list",
	Short: "List the volumes of the linked project",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireAuth(); err != nil {
			return err
		}
		projectID, err := requireProject()
		if err != nil {
			return err
		}
		client := api.NewClient()
		var volumes []api.Volume
		if err := ui.RunWithSpinner("Loading volumes...", func() error {
			var apiErr error
			volumes, apiErr = client.ListProjectVolumes(projectID)
			return apiErr
		}); err != nil {
			return fmt.Errorf("list volumes: %w", err)
		}

		if getOutputFormat() == ui.FormatJSON {
			ui.PrintJSON(volumes)
			return nil
		}
		if len(volumes) == 0 {
			ui.PrintInfo("no volumes in this project — create one with 'upuai volume add --path /data'")
			return nil
		}
		services, _ := client.ListServices(projectID)
		table := ui.NewTable("Name", "Size", "Mounted on", "ID")
		for _, v := range volumes {
			mounts := make([]string, 0, len(v.Instances))
			for _, inst := range v.Instances {
				mounts = append(mounts, serviceLabel(services, inst.ServiceID)+":"+inst.MountPath)
			}
			mounted := "—"
			if len(mounts) > 0 {
				mounted = strings.Join(mounts, ", ")
			}
			table.AddRow(v.Name, formatVolumeSize(v.SizeMb), mounted, v.ID)
		}
		table.Print()
		return nil
	},
}

var volumeAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Create a persistent disk and mount it on a service",
	Long: `Create a persistent disk and mount it on a service of the linked project.

  upuai volume add --path /data --size 5
  upuai volume add --path /var/lib/postgresql/data --size 10 -s my-app -e production

The mount path must be absolute and outside the container's system directories.
Creating the disk makes the service single-replica and switches its deploys to
stop-the-old-pod-first.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireAuth(); err != nil {
			return err
		}
		if !strings.HasPrefix(volumeMountPath, "/") {
			return fmt.Errorf("--path must be an absolute path (e.g. /data)")
		}
		if volumeSizeGb < 1 {
			return fmt.Errorf("--size must be at least 1 (GB)")
		}
		projectID, err := requireProject()
		if err != nil {
			return err
		}
		client := api.NewClient()
		envID, err := resolveEnvironmentID(client, projectID)
		if err != nil {
			return err
		}
		services, err := client.ListServices(projectID)
		if err != nil {
			return fmt.Errorf("list services: %w", err)
		}
		ref := volumeServiceRef
		if ref == "" {
			ref = linkedServiceForTarget()
		}
		if ref == "" {
			return fmt.Errorf("--service is required (no linked service in this directory)")
		}
		svc, err := matchServiceRef(services, ref)
		if err != nil {
			return err
		}

		if !flagYes {
			ui.PrintWarning(fmt.Sprintf(
				"Mounting a disk on %q makes it single-replica, and every deploy will stop the old pod before starting the new one.",
				svc.Name,
			))
			confirmed, cerr := ui.Confirm("Continue?")
			if cerr != nil {
				return cerr
			}
			if !confirmed {
				ui.PrintInfo("aborted")
				return nil
			}
		}

		var volume *api.Volume
		if err := ui.RunWithSpinner("Creating volume...", func() error {
			var apiErr error
			volume, apiErr = client.CreateVolume(projectID, api.CreateVolumeRequest{
				Name:          volumeName,
				MountPath:     volumeMountPath,
				ServiceID:     svc.ID,
				EnvironmentID: envID,
				SizeMb:        volumeSizeGb * mbPerGb,
			})
			return apiErr
		}); err != nil {
			return fmt.Errorf("create volume: %w", err)
		}

		if getOutputFormat() == ui.FormatJSON {
			ui.PrintJSON(volume)
			return nil
		}
		ui.PrintSuccess("volume created and mounted")
		ui.PrintKeyValue(
			"Name", volume.Name,
			"Size", formatVolumeSize(volume.SizeMb),
			"Mount path", volumeMountPath,
			"Service", svc.Name,
			"ID", volume.ID,
		)
		return nil
	},
}

var volumeRemoveCmd = &cobra.Command{
	Use:   "remove <name|id>",
	Short: "Detach and delete a persistent disk",
	Long: `Detach the disk from its service and delete it. The files stored on it are
lost — there is no undo.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireAuth(); err != nil {
			return err
		}
		projectID, err := requireProject()
		if err != nil {
			return err
		}
		client := api.NewClient()
		volumes, err := client.ListProjectVolumes(projectID)
		if err != nil {
			return fmt.Errorf("list volumes: %w", err)
		}
		volume, err := matchVolumeRef(volumes, args[0])
		if err != nil {
			return err
		}

		if !flagYes {
			ui.PrintWarning(fmt.Sprintf("Deleting volume %q destroys the files stored on it.", volume.Name))
			confirmed, cerr := ui.Confirm("Continue?")
			if cerr != nil {
				return cerr
			}
			if !confirmed {
				ui.PrintInfo("aborted")
				return nil
			}
		}
		if err := ui.RunWithSpinner("Deleting volume...", func() error {
			return client.DeleteVolume(volume.ID)
		}); err != nil {
			return fmt.Errorf("delete volume: %w", err)
		}
		ui.PrintSuccess(fmt.Sprintf("volume %s deleted", volume.Name))
		return nil
	},
}

// matchVolumeRef resolve nome ou ID, com a mesma tolerância de caixa dos outros
// comandos que aceitam referência humana.
func matchVolumeRef(volumes []api.Volume, ref string) (api.Volume, error) {
	for _, v := range volumes {
		if v.ID == ref || strings.EqualFold(v.Name, ref) {
			return v, nil
		}
	}
	return api.Volume{}, fmt.Errorf("volume %q not found in project — try 'upuai volume list'", ref)
}

// serviceLabel troca o ID pelo nome quando ele é conhecido; o ID sozinho não diz
// nada numa tabela.
func serviceLabel(services []api.AppService, serviceID string) string {
	for _, s := range services {
		if s.ID == serviceID {
			return s.Name
		}
	}
	return serviceID
}

func formatVolumeSize(sizeMb int) string {
	if sizeMb <= 0 {
		return "—"
	}
	if sizeMb%mbPerGb == 0 {
		return fmt.Sprintf("%d GB", sizeMb/mbPerGb)
	}
	return fmt.Sprintf("%d MB", sizeMb)
}

func init() {
	volumeAddCmd.Flags().StringVar(&volumeMountPath, "path", "", "Absolute mount path inside the container (required)")
	volumeAddCmd.Flags().IntVar(&volumeSizeGb, "size", 1, "Disk size in GB")
	volumeAddCmd.Flags().StringVar(&volumeName, "name", "", "Volume name (defaults to one derived from the path)")
	for _, c := range []*cobra.Command{volumeAddCmd} {
		c.Flags().StringVarP(&volumeServiceRef, "service", "s", "", "Service name, slug, or ID (overrides linked service)")
	}

	volumeCmd.AddCommand(volumeListCmd)
	volumeCmd.AddCommand(volumeAddCmd)
	volumeCmd.AddCommand(volumeRemoveCmd)
	rootCmd.AddCommand(volumeCmd)
}
