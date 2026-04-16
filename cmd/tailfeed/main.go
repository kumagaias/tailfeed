package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/kumagaias/tailfeed/internal/db"
	"github.com/kumagaias/tailfeed/internal/feed"
	"github.com/kumagaias/tailfeed/internal/mcp"
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
	root.AddCommand(addCmd(), removeCmd(), listCmd(), mcpCmd(), summaryCmd())
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

	// Print the most recent 10 existing articles before streaming new ones.
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

func openBrowserCLI(url string) {
	var cmd string
	switch runtime.GOOS {
	case "darwin":
		cmd = "open"
	case "linux":
		cmd = "xdg-open"
	default:
		cmd = "start"
	}
	if err := exec.Command(cmd, url).Run(); err != nil {
		fmt.Fprintf(os.Stderr, "open browser: %v\n", err)
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

// sampleFeeds is a curated list of popular RSS feeds for tech developers.
var sampleFeeds = []struct{ label, url string }{
	{"Hacker News", "https://news.ycombinator.com/rss"},
	{"Smashing Magazine", "https://www.smashingmagazine.com/feed/"},
	{"dev.to", "https://dev.to/feed"},
	{"GitHub Blog", "https://github.blog/feed/"},
	{"Krebs on Security", "https://krebsonsecurity.com/feed/"},
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

	sampleCmd := &cobra.Command{
		Use:   "sample",
		Short: "Register a curated set of popular tech RSS feeds",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			database, err := db.Open()
			if err != nil {
				return err
			}
			defer database.Close()

			for _, f := range sampleFeeds {
				if _, err := database.AddFeed(f.url, nil); err != nil {
					fmt.Fprintf(os.Stderr, "skip %s: %v\n", f.label, err)
					continue
				}
				fmt.Printf("Added: %s\n  %s\n", f.label, f.url)
			}
			return nil
		},
	}
	cmd.AddCommand(sampleCmd)
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

// mcpCmd manages MCP server configuration.
func mcpCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Manage MCP server configuration",
	}

	// mcp set <command> [args...] [--env KEY=VALUE] [--suggest-tool <tool>] [--summary-tool <tool>]
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

	// mcp unset
	unsetCmd := &cobra.Command{
		Use:   "unset",
		Short: "Remove the MCP server configuration",
		RunE: func(_ *cobra.Command, _ []string) error {
			return mcp.Save(nil)
		},
	}

	// mcp list
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "Show the configured MCP server",
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, err := mcp.Load()
			if err != nil {
				return err
			}
			if cfg == nil {
				fmt.Println("No MCP server configured.")
				fmt.Println("Use: tailfeed mcp set <command> [args...]")
				return nil
			}
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

	cmd.AddCommand(setCmd, unsetCmd, listCmd)
	return cmd
}

// summaryCmd summarises today's articles via MCP and prints to stdout.
func summaryCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "summary",
		Short: "Summarise today's articles via MCP and print to stdout",
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, err := mcp.Load()
			if err != nil {
				return err
			}
			if cfg == nil {
				return fmt.Errorf("mcp not configured — run: tailfeed mcp set <command> [args...]")
			}
			database, err := db.Open()
			if err != nil {
				return err
			}
			defer database.Close()

			articles, err := database.ListTodayArticles()
			if err != nil {
				return err
			}
			if len(articles) == 0 {
				fmt.Println("No articles today.")
				return nil
			}

			var sb strings.Builder
			for _, a := range articles {
				sb.WriteString(fmt.Sprintf("## %s\nURL: %s\n%s\n\n", a.Title, a.Link, a.Summary))
			}
			fmt.Fprintf(os.Stderr, "Summarising %d articles…\n", len(articles))

			text, err := mcp.Call(cfg, map[string]any{
				"question": fmt.Sprintf(`You are a senior engineer's daily briefing assistant. Summarize today's %d articles in %s for a technical audience. For each article: one-line TL;DR, key technical points as bullet list. End with a "## Today's Signal" section: 2-3 sentences on trends worth watching. Be concise, skip fluff.`, len(articles), cfg.SummaryLanguage()),
				"context":  sb.String(),
			})
			if err != nil {
				return err
			}
			fmt.Println(text)

			// Generate HTML report.
			path, err := tui.WriteSummaryHTML(text, articles)
			if err != nil {
				fmt.Fprintf(os.Stderr, "html: %v\n", err)
				return nil
			}
			fmt.Fprintf(os.Stderr, "\nreport saved → file://%s\n", path)
			fmt.Fprint(os.Stderr, "open in browser? [Y/n] ")
			ans, _ := bufio.NewReader(os.Stdin).ReadString('\n')
			ans = strings.TrimSpace(ans)
			if ans == "" || ans == "y" || ans == "Y" {
				openBrowserCLI(path)
			}
			return nil
		},
	}
}
