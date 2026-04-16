package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/kumagaias/tailfeed/internal/mcp"
)

// mcpCmd manages MCP server configuration.
func mcpCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Manage MCP server configuration",
	}

	var envPairs []string
	var language string
	setCmd := &cobra.Command{
		Use:   "set <command> [args...]",
		Short: "Register the MCP server",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			cfg := &mcp.Config{Command: args[0], Args: args[1:], Language: language}
			if len(envPairs) > 0 {
				cfg.Env = make(map[string]string, len(envPairs))
				for _, pair := range envPairs {
					k, v, _ := strings.Cut(pair, "=")
					cfg.Env[k] = v
				}
			}
			if err := mcp.Save(cfg); err != nil {
				return err
			}
			fmt.Printf("mcp: registered → %s\n", args[0])
			return nil
		},
	}
	setCmd.Flags().StringArrayVar(&envPairs, "env", nil, "environment variables (KEY=VALUE)")
	setCmd.Flags().StringVar(&language, "language", "", "summary language (default: Japanese)")

	unsetCmd := &cobra.Command{
		Use:   "unset",
		Short: "Remove the MCP server configuration",
		RunE: func(_ *cobra.Command, _ []string) error {
			return mcp.Save(nil)
		},
	}

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "Show the configured MCP server",
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, err := mcp.LoadRaw()
			if err != nil {
				return err
			}
			if cfg == nil {
				fmt.Println("No MCP server configured.")
				fmt.Println("Use: tailfeed mcp set <command> [args...]")
				return nil
			}
			status := "on"
			if cfg.Disabled {
				status = "off"
			}
			fmt.Printf("status:   %s\n", status)
			fmt.Printf("command:  %s %s\n", cfg.Command, strings.Join(cfg.Args, " "))
			fmt.Printf("language: %s\n", cfg.SummaryLanguage())
			if len(cfg.Env) > 0 {
				fmt.Println("env:")
				for k, v := range cfg.Env {
					fmt.Printf("  %s=%s\n", k, v)
				}
			}
			return nil
		},
	}

	onCmd := &cobra.Command{
		Use:   "on",
		Short: "Enable the configured MCP server",
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, err := mcp.LoadRaw()
			if err != nil {
				return err
			}
			if cfg == nil {
				return fmt.Errorf("no MCP server configured — use 'tailfeed mcp set <command> [args...]' first")
			}
			cfg.Disabled = false
			if err := mcp.Save(cfg); err != nil {
				return err
			}
			fmt.Println("mcp: on")
			return nil
		},
	}

	offCmd := &cobra.Command{
		Use:   "off",
		Short: "Disable the MCP server (falls back to tailfeed API)",
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, err := mcp.LoadRaw()
			if err != nil {
				return err
			}
			if cfg == nil {
				return fmt.Errorf("no MCP server configured — use 'tailfeed mcp set <command> [args...]' first")
			}
			cfg.Disabled = true
			if err := mcp.Save(cfg); err != nil {
				return err
			}
			fmt.Println("mcp: off")
			return nil
		},
	}

	cmd.AddCommand(setCmd, unsetCmd, listCmd, onCmd, offCmd)
	return cmd
}
