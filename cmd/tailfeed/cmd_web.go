package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/spf13/cobra"

	"github.com/kumagaias/tailfeed/internal/db"
	"github.com/kumagaias/tailfeed/internal/feed"
	"github.com/kumagaias/tailfeed/internal/web"
)

func webCmd() *cobra.Command {
	var addr string
	cmd := &cobra.Command{
		Use:   "web",
		Short: "Open a local browser UI",
		RunE: func(_ *cobra.Command, _ []string) error {
			return runWeb(addr)
		},
	}
	cmd.Flags().StringVar(&addr, "addr", "127.0.0.1:8080", "listen address")
	return cmd
}

func runWeb(addr string) error {
	database, err := db.Open()
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer database.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	poller := feed.New(database)
	go poller.Start(ctx)

	fmt.Fprintf(os.Stderr, "tailfeed web: %s\n", web.LocalURL(addr))
	return web.New(database).ListenAndServe(ctx, addr)
}
