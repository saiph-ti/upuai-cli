package cmd

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/spf13/cobra"
	"github.com/upuai-cloud/cli/internal/api"
	"github.com/upuai-cloud/cli/internal/config"
	"github.com/upuai-cloud/cli/internal/ui"
)

var (
	tcpProxyService  string
	tcpProxyPort     int
	tcpProxyAllow    []string
	tcpProxyAllowAny bool
)

var tcpProxyCmd = &cobra.Command{
	Use:   "tcp-proxy",
	Short: "Public TCP port for a web service (raw TCP, no HTTP/TLS from the platform)",
	Long: `Publish a public TCP port that forwards raw TCP to the linked web service —
for what does not speak HTTPS: devices on ws://, MQTT, custom protocols.

  upuai tcp-proxy                     show the current state
  upuai tcp-proxy enable              publish (application port = the service port)
  upuai tcp-proxy enable --port 9000  forward to another port of the app
  upuai tcp-proxy enable --allow 203.0.113.7 --allow 198.51.100.0/24
  upuai tcp-proxy disable

The public port is reserved for the service and stays the same across
disable/enable. Traffic is raw TCP: do TLS in the application if the protocol
needs it. Every source IP is capped at a number of concurrent connections.
Paid plans; enabling, updating and disabling need the owner or admin role
(API tokens: deploy scope). CLI v0.31.0+.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runTcpProxyStatus()
	},
}

var tcpProxyStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show the public TCP proxy of the linked web service",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runTcpProxyStatus()
	},
}

func runTcpProxyStatus() error {
	if err := requireAuth(); err != nil {
		return err
	}
	envID, serviceID, err := resolveServiceContext(tcpProxyService)
	if err != nil {
		return err
	}
	info, err := api.NewClient().GetTcpProxy(envID, serviceID)
	if err != nil {
		return explainTcpProxyError(err, "read the TCP proxy", false)
	}
	printTcpProxy(info, getOutputFormat())
	return nil
}

var tcpProxyEnableCmd = &cobra.Command{
	Use:   "enable",
	Short: "Publish (or update) the public TCP port of the linked web service",
	Long: `Publish the public TCP port, or update an enabled one.

  --port N    application port inside the container (default: the service port)
  --allow X   only these origins connect (IP or CIDR, repeatable)
  --any       open to any IP

Without --allow/--any, an enabled proxy keeps its current allowlist: a
restricted port is never opened by omission.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireAuth(); err != nil {
			return err
		}
		if cmd.Flags().Changed("port") && (tcpProxyPort < 1 || tcpProxyPort > 65535) {
			return fmt.Errorf("--port must be between 1 and 65535")
		}
		envID, serviceID, err := resolveServiceContext(tcpProxyService)
		if err != nil {
			return err
		}
		client := api.NewClient()
		current, err := client.GetTcpProxy(envID, serviceID)
		if err != nil {
			return explainTcpProxyError(err, "read the TCP proxy", false)
		}
		allow, err := resolveTcpProxyAllowList(current)
		if err != nil {
			return err
		}
		port := tcpProxyPort
		if !cmd.Flags().Changed("port") && current.Enabled {
			// Ajuste de allowlist num proxy ligado não troca a porta da app.
			port = current.ApplicationPort
		}
		var info *api.TcpProxyInfo
		if err := ui.RunWithSpinner("Publishing TCP port...", func() error {
			var apiErr error
			info, apiErr = client.SetTcpProxy(envID, serviceID, true, port, allow)
			return apiErr
		}); err != nil {
			return explainTcpProxyError(err, "enable the TCP proxy", true)
		}
		ui.PrintSuccess("TCP proxy enabled")
		printTcpProxy(info, getOutputFormat())
		return nil
	},
}

var tcpProxyDisableCmd = &cobra.Command{
	Use:   "disable",
	Short: "Remove the public TCP port (the port stays reserved for the service)",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireAuth(); err != nil {
			return err
		}
		envID, serviceID, err := resolveServiceContext(tcpProxyService)
		if err != nil {
			return err
		}
		var info *api.TcpProxyInfo
		if err := ui.RunWithSpinner("Removing TCP port...", func() error {
			var apiErr error
			info, apiErr = api.NewClient().SetTcpProxy(envID, serviceID, false, 0, nil)
			return apiErr
		}); err != nil {
			return explainTcpProxyError(err, "disable the TCP proxy", true)
		}
		ui.PrintSuccess("TCP proxy disabled")
		printTcpProxy(info, getOutputFormat())
		return nil
	},
}

// resolveTcpProxyAllowList: --any zera (aberto), --allow substitui, nenhum dos
// dois PRESERVA a lista de um proxy ligado — mesma regra do `db public enable`.
func resolveTcpProxyAllowList(current *api.TcpProxyInfo) ([]string, error) {
	if tcpProxyAllowAny {
		if len(tcpProxyAllow) > 0 {
			return nil, fmt.Errorf("--any and --allow are mutually exclusive")
		}
		return nil, nil
	}
	if len(tcpProxyAllow) > 0 {
		return normalizeAllowCIDRs(tcpProxyAllow)
	}
	if current != nil && current.Enabled {
		return current.AllowedCidrs, nil
	}
	return nil, nil
}

func printTcpProxy(info *api.TcpProxyInfo, format ui.OutputFormat) {
	if format == ui.FormatJSON {
		ui.PrintJSON(info)
		return
	}
	fmt.Println()
	if !info.Enabled {
		ui.PrintKeyValue("TCP proxy", "disabled")
		fmt.Println()
		ui.PrintInfo("Publish with: " + ui.Accent.Render("upuai tcp-proxy enable"))
		fmt.Println()
		return
	}
	allowed := "any IP"
	if len(info.AllowedCidrs) > 0 {
		allowed = strings.Join(info.AllowedCidrs, ", ")
	}
	ui.PrintKeyValue(
		"TCP proxy", "enabled",
		"Address", fmt.Sprintf("%s:%d", info.Host, info.Port),
		"Application port", fmt.Sprintf("%d", info.ApplicationPort),
		"Allowed from", allowed,
		"Max connections per IP", fmt.Sprintf("%d", info.MaxConnectionsPerIP),
	)
	fmt.Println()
	ui.PrintInfo("Raw TCP: the platform does not terminate TLS on this port.")
	fmt.Println()
}

// explainTcpProxyError traduz o 403 sem código (token READ / papel sem
// permissão) e cai no tradutor de erros de catálogo para o resto.
func explainTcpProxyError(err error, action string, write bool) error {
	if write && api.StatusCode(err) == http.StatusForbidden && api.ErrorCode(err) == "" {
		msg := "changing the TCP proxy requires the owner or admin role in this workspace"
		if config.MachineTokenFromEnv() != "" {
			msg = fmt.Sprintf("this token is read-only — changing the TCP proxy needs a token with deploy scope (%s: upuai token create --scope deploy)", config.EnvTokenVar)
		}
		var apiErr *api.APIError
		if errors.As(err, &apiErr) && apiErr.RequestID != "" {
			msg += fmt.Sprintf(" (requestId: %s)", apiErr.RequestID)
		}
		return errors.New(msg)
	}
	return fmt.Errorf("failed to %s: %w", action, err)
}

func init() {
	tcpProxyCmd.PersistentFlags().StringVarP(&tcpProxyService, "service", "s", "", "Service name, slug, or ID (overrides linked service)")
	tcpProxyEnableCmd.Flags().IntVar(&tcpProxyPort, "port", 0, "Application port inside the container (default: the service port)")
	tcpProxyEnableCmd.Flags().StringArrayVar(&tcpProxyAllow, "allow", nil, "Allowed origin, IP or CIDR (repeatable)")
	tcpProxyEnableCmd.Flags().BoolVar(&tcpProxyAllowAny, "any", false, "Open to any IP")
	tcpProxyCmd.AddCommand(tcpProxyStatusCmd, tcpProxyEnableCmd, tcpProxyDisableCmd)
	rootCmd.AddCommand(tcpProxyCmd)
}
