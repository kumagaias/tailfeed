package main

import (
	"context"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/kumagaias/tailfeed/internal/db"
	"github.com/kumagaias/tailfeed/internal/feed"
	"github.com/kumagaias/tailfeed/internal/tui"
)

var version = "dev"

func main() {
	if err := rootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func rootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:     "tailfeed",
		Short:   "A tail-style terminal RSS reader",
		Version: version,
		RunE:    runTUI,
	}
	root.AddCommand(addCmd(), removeCmd(), listCmd())
	return root
}

// runTUI opens the interactive feed viewer.
func runTUI(_ *cobra.Command, _ []string) error {
	database, err := db.Open()
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer database.Close()

	poller := feed.New(database)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go poller.Start(ctx)

	model, err := tui.New(database, poller)
	if err != nil {
		return fmt.Errorf("init tui: %w", err)
	}

	p := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		return err
	}
	return nil
}

// addCmd adds a feed from the command line.
func addCmd() *cobra.Command {
	var groupName string
	cmd := &cobra.Command{
		Use:   "add <url>",
		Short: "Register an RSS feed",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			database, err := db.Open()
			if err != nil {
				return err
			}
			defer database.Close()

			var groupID *int64
			if groupName != "" {
				g, err := database.GetGroupByName(groupName)
				if err != nil {
					return fmt.Errorf("group %q not found (create it first with: tailfeed group new %q)", groupName, groupName)
				}
				groupID = &g.ID
			}

			if _, err := database.AddFeed(args[0], groupID); err != nil {
				return err
			}
			fmt.Printf("Added: %s\n", args[0])
			return nil
		},
	}
	cmd.Flags().StringVarP(&groupName, "group", "g", "", "group name")
	return cmd
}

// removeCmd removes a feed.
func removeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <url>",
		Short: "Unregister an RSS feed",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			database, err := db.Open()
			if err != nil {
				return err
			}
			defer database.Close()
			if err := database.RemoveFeed(args[0]); err != nil {
				return err
			}
			fmt.Printf("Removed: %s\n", args[0])
			return nil
		},
	}
}

// listCmd lists all registered feeds.
func listCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List registered feeds",
		RunE: func(_ *cobra.Command, _ []string) error {
			database, err := db.Open()
			if err != nil {
				return err
			}
			defer database.Close()
			feeds, err := database.ListFeeds(nil)
			if err != nil {
				return err
			}
			if len(feeds) == 0 {
				fmt.Println("No feeds registered. Use: tailfeed add <url>")
				return nil
			}
			for _, f := range feeds {
				title := f.Title
				if title == "" {
					title = f.URL
				}
				fmt.Printf("  %s\n    %s\n", title, f.URL)
			}
			return nil
		},
	}
}
