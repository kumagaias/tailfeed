package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/kumagaias/tailfeed/internal/api"
	"github.com/kumagaias/tailfeed/internal/db"
	"github.com/kumagaias/tailfeed/internal/mcp"
	"github.com/kumagaias/tailfeed/internal/tui"
)

var reHTML = regexp.MustCompile(`<[^>]+>`)
var reWS = regexp.MustCompile(`\s+`)

// plainSummary strips HTML tags, collapses whitespace, and truncates to max runes.
func plainSummary(s string, max int) string {
	s = reHTML.ReplaceAllString(s, " ")
	s = reWS.ReplaceAllString(strings.TrimSpace(s), " ")
	runes := []rune(s)
	if len(runes) > max {
		return string(runes[:max])
	}
	return s
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

type sampleFeed struct{ label, url string }

// availableLangs returns a sorted comma-separated list of supported language codes.
func availableLangs() string {
	keys := make([]string, 0, len(sampleFeedsByLang))
	for k := range sampleFeedsByLang {
		keys = append(keys, k)
	}
	return strings.Join(keys, ", ")
}

// sampleFeedsByLang holds curated feeds keyed by language code.
var sampleFeedsByLang = map[string][]sampleFeed{
	"en": {
		{"Hacker News", "https://news.ycombinator.com/rss"},
		{"Smashing Magazine", "https://www.smashingmagazine.com/feed/"},
		{"dev.to", "https://dev.to/feed"},
		{"GitHub Blog", "https://github.blog/feed/"},
		{"Krebs on Security", "https://krebsonsecurity.com/feed/"},
	},
	"ja": {
		{"Zenn トレンド", "https://zenn.dev/feed"},
		{"Qiita トレンド", "https://qiita.com/popular-items/feed"},
		{"はてなブックマーク IT", "https://b.hatena.ne.jp/hotentry/it.rss"},
		{"gihyo.jp", "https://gihyo.jp/feed/atom"},
		{"NHK IT・ネット", "https://www3.nhk.or.jp/rss/news/cat06.xml"},
	},
}

func addCmd() *cobra.Command {
	var groupName string
	var lang string
	cmd := &cobra.Command{
		Use:   "add <url|sample>",
		Short: "Register an RSS feed (use \"sample\" to add popular feeds)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			database, err := db.Open()
			if err != nil {
				return err
			}
			defer database.Close()

			if args[0] == "sample" {
				feeds, ok := sampleFeedsByLang[lang]
				if !ok {
					return fmt.Errorf("unknown language %q (available: %s)", lang, availableLangs())
				}
				for _, f := range feeds {
					if _, err := database.AddFeed(f.url, nil); err != nil {
						fmt.Fprintf(os.Stderr, "skip %s: %v\n", f.label, err)
						continue
					}
					fmt.Printf("Added: %s\n  %s\n", f.label, f.url)
				}
				return nil
			}

			// Validate URL format
			if !strings.HasPrefix(args[0], "http://") && !strings.HasPrefix(args[0], "https://") {
				return fmt.Errorf("invalid URL format: %q (must start with http:// or https://)", args[0])
			}

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
	cmd.Flags().StringVar(&lang, "lang", "en", "language for sample feeds (en, ja)")
	return cmd
}

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

func summaryCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "summary [today|yesterday|week]",
		Short: "Summarise articles via MCP (or tailfeed API) and print to stdout",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			period := "today"
			if len(args) > 0 {
				period = args[0]
			}

			database, err := db.Open()
			if err != nil {
				return err
			}
			defer database.Close()

			var articles []db.Article
			var label string
			switch period {
			case "yesterday":
				articles, err = database.ListYesterdayArticles()
				label = "yesterday"
			case "week":
				articles, err = database.ListWeekArticles()
				label = "last 7 days"
			default:
				articles, err = database.ListTodayArticles()
				label = "today"
			}
			if err != nil {
				return err
			}
			if len(articles) == 0 {
				fmt.Printf("No articles for %s.\n", label)
				return nil
			}
			fmt.Fprintf(os.Stderr, "Summarising %d articles (%s)…\n", len(articles), label)

			var text string
			mcpCfg, err := mcp.Load()
			if err != nil {
				return err
			}
			if mcpCfg != nil {
				var sb strings.Builder
				for _, a := range articles {
					sb.WriteString(fmt.Sprintf("## %s\nURL: %s\n%s\n\n", a.Title, a.Link, a.Summary))
				}
				text, err = mcp.Call(mcpCfg, map[string]any{
					"question": fmt.Sprintf(`You are a senior engineer's daily briefing assistant. Summarize %s's %d articles in %s for a technical audience. For each article: one-line TL;DR, key technical points as bullet list. End with a "## Today's Signal" section: 2-3 sentences on trends worth watching. Be concise, skip fluff.`, label, len(articles), mcpCfg.SummaryLanguage()),
					"context":  sb.String(),
				})
				if err != nil {
					return err
				}
			} else {
				apiCfg, err := api.LoadOrRegister()
				if err != nil {
					return err
				}
				apiArticles := make([]api.SummaryArticle, len(articles))
				for i, a := range articles {
					apiArticles[i] = api.SummaryArticle{Title: a.Title, URL: a.Link, Summary: plainSummary(a.Summary, 300)}
				}
				text, err = api.Summary(apiCfg.UserKey, apiArticles, "Japanese")
				if err != nil {
					return err
				}
			}

			fmt.Println(text)

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

func usageCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "usage",
		Short: "Show plan and remaining API quota",
		RunE: func(_ *cobra.Command, _ []string) error {
			apiCfg, err := api.LoadOrRegister()
			if err != nil {
				return err
			}
			info, err := api.Usage(apiCfg.UserKey)
			if err != nil {
				return err
			}
			fmt.Printf("plan:    %s\n", info.Plan)
			fmt.Printf("usage:   %d / %d remaining (summary, suggest)\n", info.SummaryRemaining, info.SummaryLimit)
			if info.ResetAt != "" {
				fmt.Printf("resets:  %s\n", info.ResetAt)
			}
			return nil
		},
	}
}

func clearCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "clear",
		Short: "Remove all feeds, articles, and groups from the database",
		RunE: func(_ *cobra.Command, _ []string) error {
			if !yes {
				fmt.Print("This will delete all feeds, articles, and groups. Continue? [y/N] ")
				ans, _ := bufio.NewReader(os.Stdin).ReadString('\n')
				if strings.ToLower(strings.TrimSpace(ans)) != "y" {
					fmt.Println("cancelled")
					return nil
				}
			}
			database, err := db.Open()
			if err != nil {
				return err
			}
			defer database.Close()
			if err := database.Clear(); err != nil {
				return err
			}
			fmt.Println("cleared: all feeds, articles, and groups removed")
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "skip confirmation prompt")
	return cmd
}

func registerCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "register",
		Short: "Register with the tailfeed API and save your API key",
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, err := api.Register()
			if err != nil {
				return err
			}
			if err := api.Save(cfg); err != nil {
				return err
			}
			fmt.Printf("registered: user_key=%s  tier=%s\n", cfg.UserKey, cfg.Tier)
			fmt.Println("key saved → ~/.config/tailfeed/api.json")
			return nil
		},
	}
}
