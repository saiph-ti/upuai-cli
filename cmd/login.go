package cmd

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/pkg/browser"
	"github.com/spf13/cobra"
	"github.com/upuai-cloud/cli/internal/api"
	"github.com/upuai-cloud/cli/internal/auth"
	"github.com/upuai-cloud/cli/internal/config"
	"github.com/upuai-cloud/cli/internal/ui"
)

var (
	loginEmailFlag  bool
	loginGithubFlag bool
)

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Authenticate with Upuai Cloud",
	Long: `Authenticate with Upuai Cloud.

By default, opens the browser for one-click authorization.
Use --email for email-based OTP login.
Use --github for GitHub OAuth authentication.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if loginEmailFlag {
			return loginWithEmail()
		}
		if loginGithubFlag {
			return loginWithOAuth()
		}
		return loginWithBrowser()
	},
}

func init() {
	loginCmd.Flags().BoolVar(&loginEmailFlag, "email", false, "Login via email OTP code")
	loginCmd.Flags().BoolVar(&loginGithubFlag, "github", false, "Login via GitHub OAuth")
	rootCmd.AddCommand(loginCmd)
}

func loginWithBrowser() error {
	ui.PrintBanner()

	confirmed, err := ui.Confirm("Open the browser to log in?")
	if err != nil {
		return err
	}
	if !confirmed {
		fmt.Println()
		ui.PrintInfo("You can also log in with:")
		fmt.Println("  upuai login --email    Login via email OTP code")
		fmt.Println("  upuai login --github   Login via GitHub OAuth")
		return nil
	}

	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return fmt.Errorf("failed to generate session token: %w", err)
	}
	sessionToken := hex.EncodeToString(tokenBytes)

	client := api.NewClient()

	var serverSessionToken string
	err = ui.RunWithSpinner("Preparing login session...", func() error {
		var initErr error
		serverSessionToken, initErr = client.InitCliSession(sessionToken)
		return initErr
	})
	if err != nil {
		return fmt.Errorf("failed to create login session: %w", err)
	}

	webURL := config.GetWebURL()
	authURL := fmt.Sprintf("%s/auth/cli?session=%s", webURL, serverSessionToken)

	if err := browser.OpenURL(authURL); err != nil {
		fmt.Println()
		ui.PrintWarning("Could not open browser automatically.")
		fmt.Println("  Open this URL in your browser:")
		fmt.Println("  " + ui.Accent.Render(authURL))
		fmt.Println()
	}

	var loginResp *api.CliSessionStatusResponse
	err = ui.RunWithSpinner("Waiting for authorization in browser...", func() error {
		timeout := time.After(5 * time.Minute)
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-timeout:
				return fmt.Errorf("authorization timed out after 5 minutes")
			case <-ticker.C:
				status, err := client.GetCliSessionStatus(serverSessionToken)
				if err != nil {
					return fmt.Errorf("failed to check session status: %w", err)
				}
				switch status.Status {
				case "approved":
					loginResp = status
					return nil
				case "expired":
					return fmt.Errorf("session expired, please try again")
				case "pending":
					continue
				default:
					return fmt.Errorf("unexpected session status: %s", status.Status)
				}
			}
		}
	})
	if err != nil {
		return fmt.Errorf("authorization failed: %w", err)
	}

	return saveLoginResponse(&api.LoginResponse{
		UserID:       loginResp.UserID,
		UserName:     loginResp.UserName,
		Login:        loginResp.Login,
		Token:        loginResp.Token,
		RefreshToken: loginResp.RefreshToken,
		TenantPlan:   loginResp.TenantPlan,
		AvatarUrl:    loginResp.AvatarUrl,
	})
}

func loginWithOAuth() error {
	ui.PrintBanner()
	fmt.Println("  Opening browser for GitHub authentication...")
	fmt.Println()

	apiURL := config.GetAPIURL()
	authURL := apiURL + "/auth/oauth/github"

	result, err := auth.StartOAuthFlow(authURL)
	if err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}

	client := api.NewClient()
	var loginResp *api.LoginResponse

	err = ui.RunWithSpinner("Authenticating...", func() error {
		var loginErr error
		loginResp, loginErr = client.LoginOAuthGitHub(result.Code, result.RedirectURI)
		return loginErr
	})
	if err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}

	return saveLoginResponse(loginResp)
}

func loginWithEmail() error {
	ui.PrintBanner()

	email, err := ui.InputText("Email address", "you@example.com")
	if err != nil {
		return err
	}
	if email == "" {
		return fmt.Errorf("email is required")
	}

	client := api.NewClient()

	err = ui.RunWithSpinner("Sending verification code...", func() error {
		return client.SendEmailToken(email)
	})
	if err != nil {
		return fmt.Errorf("failed to send code: %w", err)
	}

	ui.PrintSuccess("Verification code sent to " + email)
	fmt.Println()

	code, err := ui.InputText("Enter the 6-digit code", "000000")
	if err != nil {
		return err
	}
	if code == "" {
		return fmt.Errorf("verification code is required")
	}

	var loginResp *api.LoginResponse
	err = ui.RunWithSpinner("Verifying...", func() error {
		var loginErr error
		loginResp, loginErr = client.LoginWithEmailToken(email, code)
		return loginErr
	})
	if err != nil {
		return fmt.Errorf("verification failed: %w", err)
	}

	return saveLoginResponse(loginResp)
}

func saveLoginResponse(resp *api.LoginResponse) error {
	store := config.NewCredentialStore()
	creds := &config.Credentials{
		Token:        resp.Token,
		RefreshToken: resp.RefreshToken,
		User: config.StoredUser{
			UserID:   resp.UserID,
			UserName: resp.UserName,
			Login:    resp.Login,
		},
		ApiURL: config.GetAPIURL(),
	}
	if err := store.Save(creds); err != nil {
		return fmt.Errorf("failed to save credentials: %w", err)
	}

	fmt.Println()
	ui.PrintSuccess("Logged in as " + ui.Accent.Render(resp.UserName) + " (" + resp.Login + ")")
	return nil
}
