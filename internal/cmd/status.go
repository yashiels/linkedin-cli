package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/yashiels/linkedin-cli/internal/auth"
	"github.com/yashiels/linkedin-cli/internal/config"
)

// NewStatusCmd returns the "lnk status" command.
func NewStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show API and auth status",
		Long: `Display the current authentication state, configuration path, and
API connectivity. Useful for verifying your setup.

Example output:
  ✓ Logged in as Yashiel Sookdeo
    Session: active
    Config:  ~/.config/lnk/config.toml
    API:     connected (prod-ltx1)`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStatus(cmd)
		},
	}
}

func runStatus(cmd *cobra.Command) error {
	w := cmd.OutOrStdout()
	jsonMode := isJSONMode(cmd)

	// --- Auth ---
	store, err := auth.Default()
	if err != nil {
		return fmt.Errorf("reading auth store: %w", err)
	}
	creds, err := store.Load()
	if err != nil {
		return fmt.Errorf("loading credentials: %w", err)
	}

	loggedIn := creds.LiAt != "" && creds.CSRFToken != ""

	// --- Config ---
	cfgPath, _ := config.ConfigPath()
	cfgExists := false
	if _, statErr := os.Stat(cfgPath); statErr == nil {
		cfgExists = true
	}

	// --- API connectivity (only when logged in) ---
	var apiStatus, apiPop, profileName string
	if loggedIn {
		client, clientErr := newAPIClient(cmd)
		if clientErr == nil {
			pop, pingErr := client.Ping()
			if pingErr == nil {
				apiStatus = "connected"
				apiPop = pop
			} else {
				apiStatus = "error: " + pingErr.Error()
			}

			// Best-effort: fetch own name.
			if apiStatus == "connected" {
				meRaw, meErr := client.Get("/voyager/api/me", nil)
				if meErr == nil {
					firstName := jstr(jget(meRaw, "miniProfile", "firstName"))
					lastName := jstr(jget(meRaw, "miniProfile", "lastName"))
					if firstName != "" || lastName != "" {
						profileName = strings.TrimSpace(firstName + " " + lastName)
					}
				}
			}
		} else {
			apiStatus = "error: " + clientErr.Error()
		}
	}

	if jsonMode {
		out := newOutputWriter(cmd)
		payload := map[string]interface{}{
			"loggedIn":    loggedIn,
			"configPath":  cfgPath,
			"configFound": cfgExists,
			"apiStatus":   apiStatus,
			"apiPop":      apiPop,
		}
		if profileName != "" {
			payload["name"] = profileName
		}
		return out.JSON(payload)
	}

	// Human-readable output.
	if loggedIn {
		nameStr := "Logged in"
		if profileName != "" {
			nameStr = "Logged in as " + profileName
		}
		fmt.Fprintln(w, "✓ "+nameStr)
		fmt.Fprintln(w, "  Session: active")
	} else {
		fmt.Fprintln(w, "✗ Not logged in")
		fmt.Fprintln(w, "  Run: lnk auth login")
	}

	// Config.
	cfgDisplay := tildeHome(cfgPath)
	if cfgExists {
		fmt.Fprintf(w, "  Config:  %s\n", cfgDisplay)
	} else {
		fmt.Fprintf(w, "  Config:  %s (not found — using defaults)\n", cfgDisplay)
	}

	// API.
	if loggedIn {
		apiLine := apiStatus
		if apiPop != "" && apiStatus == "connected" {
			apiLine = "connected (" + apiPop + ")"
		}
		fmt.Fprintf(w, "  API:     %s\n", apiLine)
	}

	return nil
}

// tildeHome replaces the home directory prefix with "~" for display.
func tildeHome(path string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if strings.HasPrefix(path, home+"/") {
		return "~/" + path[len(home)+1:]
	}
	return path
}
