package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

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
	root.AddCommand(addCmd(), removeCmd(), listCmd(), mcpCmd(), summaryCmd(), registerCmd(), clearCmd(), usageCmd())
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

	var recentGroupID *int64
	if groupName != "" {
		g, _ := database.GetGroupByName(groupName)
		recentGroupID = &g.ID
	}
	recent, err := database.ListRecentArticles(recentGroupID, 10)
	if err == nil && len(recent) > 0 {
		fmt.Fprintln(os.Stderr, "── recent articles ──────────────────────────────────────────")
		for _, a := range recent {
			printArticle(a)
		}
		fmt.Fprintln(os.Stderr, "── watching for new articles ────────────────────────────────")
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
