package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"

	"golang.org/x/sync/errgroup"

	"github.com/ardnew/relay/server"
)

const defaultPort = 50135

var defaultShells = []string{"sh", "bash", "zsh"}

func main() {
	port := flag.Int("p", defaultPort, "base port to listen on")
	json := flag.Bool("j", false, "use JSON structured logging")

	flag.Usage = func() {
		out := flag.CommandLine.Output()
		fmt.Fprintf(out, "Usage: %s [options] [shells...]\n", os.Args[0])
		fmt.Fprintln(out, "Options:")
		flag.PrintDefaults()
	}

	flag.Parse()

	shells := flag.Args()
	if len(shells) == 0 {
		shells = defaultShells
	}

	// Parent context all goroutine contexts will derive from.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Cancelling this context via signal handler will shut down all servers.
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt)

	var handler slog.Handler = slog.NewTextHandler(os.Stdout, nil)
	if *json {
		handler = slog.NewJSONHandler(os.Stdout, nil)
	}

	log := slog.New(handler)

	go func() {
		<-signalChan
		log.InfoContext(ctx, "shutting down")
		cancel()
	}()

	servers := server.Make(ctx, log, *port, shells...)

	var g errgroup.Group
	for _, srv := range servers {
		g.Go(func() error { return srv.ListenAndServe(ctx) })
	}
	if g.Wait() != nil {
		log.ErrorContext(ctx, "exiting due to error")
	}
}
