package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"time"

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
	var follow bool
	var groupName string

	root := &cobra.Command{
		Use:     "tailfeed",
		Short:   "A tail-style terminal RSS reader",
		Version: version,
		RunE: func(cmd *cobra.Command, args []string) error {
			if follow {
				return runFollow(groupName)
			}
			return runTUI()
		},
	}
	root.Flags().BoolVarP(&follow, "follow", "f", false, "stream new articles to stdout (tail -f style)")
	root.Flags().StringVarP(&groupName, "group", "g", "", "filter by group name (use with -f)")
	root.AddCommand(addCmd(), removeCmd(), listCmd())
	return root
}

// runTUI opens the interactive feed viewer.
func runTUI() error {
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

	p := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		return err
	}
	return nil
}

// runFollow streams new articles to stdout, like tail -f.
func runFollow(groupName string) error {
	database, err := db.Open()
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer database.Close()

	// Build the set of allowed feed IDs when filtering by group.
	var allowedFeeds map[int64]bool
	if groupName != "" {
		g, err := database.GetGroupByName(groupName)
		if err != nil {
			return fmt.Errorf("group %q not found", groupName)
		}
		feeds, err := database.ListFeeds(&g.ID)
		if err != nil {
			return err
		}
		allowedFeeds = make(map[int64]bool, len(feeds))
		for _, f := range feeds {
			allowedFeeds[f.ID] = true
		}
		fmt.Fprintf(os.Stderr, "tailfeed: watching group %q (%d feeds) — Ctrl+C to stop\n", groupName, len(feeds))
	} else {
		fmt.Fprintln(os.Stderr, "tailfeed: watching all feeds — Ctrl+C to stop")
	}

	poller := feed.New(database)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	go func() {
		<-sigCh
		fmt.Fprintln(os.Stderr, "\ntailfeeed: stopped")
		cancel()
	}()

	go poller.Start(ctx)

	for {
		select {
		case <-ctx.Done():
			return nil
		case a := <-poller.Articles():
			if allowedFeeds != nil && !allowedFeeds[a.FeedID] {
				continue
			}
			printArticle(db.Article(a))
		}
	}
}

func printArticle(a db.Article) {
	fmt.Println(strings.Repeat("─", 60))
	meta := a.FeedTitle
	if a.PublishedAt != nil {
		d := time.Since(*a.PublishedAt)
		var ago string
		switch {
		case d < time.Minute:
			ago = "just now"
		case d < time.Hour:
			ago = fmt.Sprintf("%dm ago", int(d.Minutes()))
		case d < 24*time.Hour:
			ago = fmt.Sprintf("%dh ago", int(d.Hours()))
		default:
			ago = fmt.Sprintf("%dd ago", int(d.Hours()/24))
		}
		meta += "  ·  " + ago
	}
	fmt.Println(meta)
	fmt.Println(a.Title)
	if a.Summary != "" {
		s := stripHTMLSimple(a.Summary)
		if len([]rune(s)) > 200 {
			s = string([]rune(s)[:197]) + "..."
		}
		fmt.Println(s)
	}
	if a.Link != "" {
		fmt.Println(a.Link)
	}
	fmt.Println()
}

// stripHTMLSimple removes HTML tags (no external dependency).
func stripHTMLSimple(s string) string {
	var b strings.Builder
	inTag := false
	for _, r := range s {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
		case !inTag:
			b.WriteRune(r)
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
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
