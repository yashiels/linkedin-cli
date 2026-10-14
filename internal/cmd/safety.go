package cmd

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/yashiels/linkedin-cli/internal/config"
	"github.com/yashiels/linkedin-cli/internal/safety"
	"github.com/yashiels/linkedin-cli/internal/types"
)

func NewSafetyCmd() *cobra.Command {
	group := &cobra.Command{Use: "safety", Short: "Inspect or resume local request safety controls"}
	var confirmed bool
	for _, name := range []string{"status", "resume"} {
		command := &cobra.Command{Use: name, Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				return types.NewError(2, "safety command accepts no arguments")
			}
			return nil
		}, Short: "Show local request safety state"}
		if name == "resume" {
			command.Short = "Clear manual-review stop after inspecting the official app"
			command.Flags().BoolVar(&confirmed, "confirm", false, "Confirm manual review; preserve cooldown and request budget")
		}

		command.RunE = func(cmd *cobra.Command, args []string) error {
			if name == "resume" && !confirmed {
				return types.NewError(2, "safety resume requires --confirm after manual review")
			}
			cfg, err := config.Load()
			if err != nil {
				return types.NewError(2, "safety configuration could not be loaded")
			}
			policy, err := safety.Default(safety.Config{Delay: time.Duration(cfg.Safety.DelaySeconds) * time.Second, Budget: cfg.Safety.Budget})
			if err != nil {
				return err
			}
			var state safety.Status
			if name == "resume" {
				state, err = policy.Resume(cmd.Context(), confirmed)
			} else {
				state, err = policy.Status(cmd.Context())
			}
			if err != nil {
				return err
			}
			jsonMode, _ := cmd.Root().PersistentFlags().GetBool("json")
			if jsonMode {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(state)
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Admissions: %d/%d (rolling 24h)\nMinimum delay: %.0fs\nManual stop: %s\nCooldown until: %s\n", state.Admissions, state.Budget, state.DelaySeconds, state.Block, state.Cooldown.UTC().Format(time.RFC3339))
			return err
		}
		group.AddCommand(command)
	}
	return group
}
