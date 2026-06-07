package cmd

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/yashiels/linkedin-cli/internal/output"
	"github.com/yashiels/linkedin-cli/internal/types"
)

// NewAlertsCmd returns the "lnk alerts" command tree.
func NewAlertsCmd() *cobra.Command {
	alertsCmd := &cobra.Command{
		Use:   "alerts",
		Short: "Manage LinkedIn job alerts",
		Long:  "List, create, and delete LinkedIn job alert subscriptions.",
	}

	alertsCmd.AddCommand(
		newAlertsListCmd(),
		newAlertsCreateCmd(),
		newAlertsDeleteCmd(),
	)
	return alertsCmd
}

// newAlertsListCmd returns "lnk alerts list".
func newAlertsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List job alerts",
		Long:  "List all active LinkedIn job alert subscriptions.",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := newAPIClient(cmd)
			if err != nil {
				return err
			}

			// Primary: GraphQL JobAlertsAll.
			raw, err := client.QueryGraphQL(
				"JobAlertsAll",
				"voyagerJobsDashJobAlerts.c059156ea2ecc4dd8cbfd324f9bf2987",
				map[string]interface{}{},
			)
			if err != nil {
				return fmt.Errorf("fetching alerts: %w", err)
			}

			alerts, err := parseAlerts(raw)
			if err != nil {
				return fmt.Errorf("parsing alerts response: %w", err)
			}

			out := newOutputWriter(cmd)

			if isJSONMode(cmd) {
				return out.JSON(alerts)
			}

			if len(alerts) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No job alerts configured.")
				fmt.Fprintln(cmd.OutOrStdout(), "Create one with: lnk alerts create --keywords <kw> --location <loc>")
				return nil
			}

			out.Table([]output.Column{
				{Header: "ALERT ID"},
				{Header: "KEYWORDS"},
				{Header: "LOCATION"},
				{Header: "FREQUENCY"},
				{Header: "CREATED"},
			}, alertsToRows(alerts))

			return nil
		},
	}
}

// newAlertsCreateCmd returns "lnk alerts create".
func newAlertsCreateCmd() *cobra.Command {
	var (
		flagKeywords  string
		flagLocation  string
		flagFrequency string
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a job alert",
		Long: `Create a new LinkedIn job alert subscription.

LinkedIn will email you matching job postings at the specified frequency.

Examples:
  lnk alerts create --keywords "software engineer" --location "Cape Town"
  lnk alerts create --keywords "product manager" --location "Remote" --frequency weekly`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if flagKeywords == "" {
				return fmt.Errorf("--keywords is required")
			}

			client, err := newAPIClient(cmd)
			if err != nil {
				return err
			}

			freq := strings.ToUpper(flagFrequency)
			if freq == "" {
				freq = "DAILY"
			}

			payload := map[string]interface{}{
				"keywords":  flagKeywords,
				"frequency": freq,
			}
			if flagLocation != "" {
				payload["location"] = flagLocation
			}

			raw, err := client.Post("/voyager/api/jobs/jobAlerts", payload)
			if err != nil {
				return fmt.Errorf("creating alert: %w", err)
			}

			alert, parseErr := parseSingleAlert(raw)
			out := newOutputWriter(cmd)

			if isJSONMode(cmd) {
				if parseErr == nil {
					return out.JSON(alert)
				}
				return out.JSON(map[string]string{"status": "created"})
			}

			fmt.Fprintln(cmd.OutOrStdout(), "✓ Job alert created.")
			if parseErr == nil && alert.ID != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "  ID:        %s\n", alert.ID)
				fmt.Fprintf(cmd.OutOrStdout(), "  Keywords:  %s\n", alert.Keywords)
				if alert.Location != "" {
					fmt.Fprintf(cmd.OutOrStdout(), "  Location:  %s\n", alert.Location)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "  Frequency: %s\n", alert.Frequency)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&flagKeywords, "keywords", "", "Job search keywords (required)")
	cmd.Flags().StringVar(&flagLocation, "location", "", "Location filter (city, country, or 'Remote')")
	cmd.Flags().StringVar(&flagFrequency, "frequency", "daily", "Email frequency: daily or weekly")
	return cmd
}

// newAlertsDeleteCmd returns "lnk alerts delete".
func newAlertsDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <alert-id>",
		Short: "Delete a job alert",
		Long: `Delete a LinkedIn job alert subscription by ID.

Use 'lnk alerts list' to find alert IDs.

Example:
  lnk alerts delete 123456789`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			alertID := args[0]

			client, err := newAPIClient(cmd)
			if err != nil {
				return err
			}

			err = client.Delete("/voyager/api/jobs/jobAlerts/" + alertID)
			if err != nil {
				return fmt.Errorf("deleting alert %s: %w", alertID, err)
			}

			out := newOutputWriter(cmd)
			if isJSONMode(cmd) {
				return out.JSON(map[string]string{"deleted": alertID})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "✓ Alert %s deleted.\n", alertID)
			return nil
		},
	}
}

// --- Parsing helpers ---

// parseAlerts extracts a slice of JobAlert from a GraphQL response.
func parseAlerts(raw json.RawMessage) ([]types.JobAlert, error) {
	if raw == nil {
		return nil, fmt.Errorf("empty response")
	}

	// Try GraphQL wrapper: data > <key> > elements.
	elems := jdataElems(raw)
	if elems == nil {
		// Try bare elements at root.
		elems = jelems(raw)
	}

	var alerts []types.JobAlert
	for _, el := range elems {
		a := parseAlertElement(el)
		alerts = append(alerts, a)
	}
	return alerts, nil
}

// parseAlertElement extracts a single JobAlert from an element node.
func parseAlertElement(el json.RawMessage) types.JobAlert {
	a := types.JobAlert{}

	// URN and ID.
	urn := firstNonEmpty(
		jstr(jget(el, "entityUrn")),
		jstr(jget(el, "jobAlertUrn")),
	)
	a.URN = urn
	if urn != "" {
		// Extract numeric ID from "urn:li:jobAlert:123456".
		parts := strings.Split(urn, ":")
		if len(parts) > 0 {
			a.ID = parts[len(parts)-1]
		}
	}
	if a.ID == "" {
		a.ID = jstr(jget(el, "id"))
	}

	// Look for the nested alert payload.
	alertNode := jget(el, "alert")
	if alertNode == nil {
		alertNode = el
	}

	a.Keywords = parseKeywords(alertNode)
	a.Location = parseAlertLocation(alertNode)
	a.Frequency = jstr(jget(alertNode, "frequency"))

	// Created timestamp (milliseconds epoch).
	tsRaw := jget(el, "createdAt")
	if tsRaw == nil {
		tsRaw = jget(alertNode, "createdAt")
	}
	if ts := jint(tsRaw); ts > 0 {
		a.CreatedAt = time.UnixMilli(int64(ts)).Format("2006-01-02")
	}

	return a
}

func parseSingleAlert(raw json.RawMessage) (types.JobAlert, error) {
	if raw == nil {
		return types.JobAlert{}, fmt.Errorf("empty response")
	}
	return parseAlertElement(raw), nil
}

func parseKeywords(node json.RawMessage) string {
	// Could be a string or an array.
	kwRaw := jget(node, "keywords")
	if kwRaw == nil {
		return ""
	}
	// Try as string.
	if s := jstr(kwRaw); s != "" {
		return s
	}
	// Try as array.
	var arr []string
	if err := json.Unmarshal(kwRaw, &arr); err == nil {
		return strings.Join(arr, ", ")
	}
	return ""
}

func parseAlertLocation(node json.RawMessage) string {
	// Try plain string.
	if loc := jstr(jget(node, "location")); loc != "" {
		return loc
	}
	// Try locationUnion > geoUrn or locationUnion > location.
	lu := jget(node, "locationUnion")
	if lu == nil {
		return ""
	}
	if loc := jstr(jget(lu, "location")); loc != "" {
		return loc
	}
	if geo := jstr(jget(lu, "geoUrn")); geo != "" {
		return geo
	}
	return ""
}

func alertsToRows(alerts []types.JobAlert) [][]string {
	rows := make([][]string, len(alerts))
	for i, a := range alerts {
		rows[i] = []string{a.ID, a.Keywords, a.Location, a.Frequency, a.CreatedAt}
	}
	return rows
}
