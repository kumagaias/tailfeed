package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

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

	listener, err := listenWeb(addr)
	if err != nil {
		return err
	}

	url := web.LocalURL(listener.Addr().String())
	fmt.Fprintf(os.Stderr, "tailfeed web: %s\n", url)
	go func() {
		time.Sleep(150 * time.Millisecond)
		openBrowserCLI(url)
	}()
	return web.New(database).Serve(ctx, listener)
}

func listenWeb(addr string) (net.Listener, error) {
	for attempt := 0; attempt < 100; attempt++ {
		listener, err := net.Listen("tcp", addr)
		if err == nil {
			return listener, nil
		}
		if !isAddrInUse(err) {
			return nil, err
		}
		next, nextErr := incrementAddrPort(addr)
		if nextErr != nil {
			return nil, err
		}
		fmt.Fprintf(os.Stderr, "tailfeed: %s is in use, trying %s\n", addr, next)
		addr = next
	}
	return nil, fmt.Errorf("no available port found near %s", addr)
}

func incrementAddrPort(addr string) (string, error) {
	host, portText, err := net.SplitHostPort(addr)
	if err != nil {
		return "", err
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port <= 0 || port >= 65535 {
		return "", fmt.Errorf("invalid port: %q", portText)
	}
	return net.JoinHostPort(host, strconv.Itoa(port+1)), nil
}

func isAddrInUse(err error) bool {
	return errors.Is(err, syscall.EADDRINUSE)
}
