package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sync/errgroup"

	"github.com/ardnew/relay/srv"
)

type exportFlag map[string]string

var (
	defaultListen = "127.0.0.1:50135"
	defaultExport = exportFlag{}
	defaultOutput = "-"
)

func (f *exportFlag) String() string {
	var sb strings.Builder
	for k, v := range *f {
		if sb.Len() > 0 {
			sb.WriteString(",")
		}

		sb.WriteString(k)
		sb.WriteRune('=')
		sb.WriteString(strconv.Quote(v))
	}

	return sb.String()
}

func (f *exportFlag) Set(s string) error {
	ident, value, found := strings.Cut(s, "=")
	if !found {
		return fmt.Errorf("invalid export format: %q (expected IDENT=VALUE)", s)
	}

	if *f == nil {
		*f = make(map[string]string)
	}

	(*f)[ident] = value

	return nil
}

// resolveShell resolves a shell name or path to an absolute path.
func resolveShell(shell string) (string, error) {
	shell = strings.TrimSpace(shell)
	if shell == "" {
		return "", errors.New("shell name is empty")
	}

	if filepath.IsAbs(shell) {
		return shell, nil
	}

	_, err := os.Stat(shell)
	if os.IsNotExist(err) {
		path, err := exec.LookPath(shell)
		if err != nil {
			return "", fmt.Errorf("shell %q not found", shell)
		}

		return path, nil
	}

	if err != nil {
		return "", fmt.Errorf("failed to stat %q: %w", shell, err)
	}

	abs, err := filepath.Abs(shell)
	if err != nil {
		return "", fmt.Errorf("cannot resolve path %q: %w", shell, err)
	}

	return abs, nil
}

// parseServiceArg parses an argument in format "shell[:addr][:port]".
func parseServiceArg(
	arg, defaultAddr string,
	defaultPort int,
) (shell, addr string, port int, err error) {
	parts := strings.Split(arg, ":")
	if len(parts) == 0 || parts[0] == "" {
		return "", "", 0, errors.New("shell name is required")
	}

	shell = parts[0]
	addr = ""
	port = 0

	// Parse based on number of parts
	switch len(parts) {
	case 1:
		// "bash" - use defaults for both
		addr = ""
		port = 0
	case 2:
		// "bash:" or "bash:192.168.0.1" or "bash:900"
		// Treat as address (will fail at bind time if invalid)
		if parts[1] != "" {
			addr = parts[1]
		}
		// else "bash:" - use defaults
	case 3:
		// "bash:192.168.0.1:8080" or "bash::8080" or "bash:192.168.0.1:" or
		// "bash::"
		if parts[1] != "" {
			addr = parts[1]
		}

		if parts[2] != "" {
			port, err = strconv.Atoi(parts[2])
			if err != nil {
				return "", "", 0, fmt.Errorf("invalid port %q: %w", parts[2], err)
			}
		}
	default:
		return "", "", 0, fmt.Errorf("invalid format %q: too many colons", arg)
	}

	// Apply defaults for unspecified values
	if addr == "" {
		addr = defaultAddr
	}

	if port == 0 {
		port = defaultPort
	}

	// Validate port range
	if port < 1 || port > 65535 {
		return "", "", 0, fmt.Errorf("port %d out of range (1-65535)", port)
	}

	shell, err = resolveShell(shell)
	if err != nil {
		return "", "", 0, err
	}

	return shell, addr, port, nil
}

func main() {
	expo := defaultExport

	listen := flag.String(
		"l",
		defaultListen,
		"default listen `[ADDR]:PORT` for shells",
	)

	flag.Var(&expo, "e", "export `IDENT=VALUE` to shells")

	output := flag.String(
		"o",
		defaultOutput,
		"append log to `FILE` (\"-\" is stdout)",
	)
	jsonFlag := flag.Bool("j", false, "use JSON structured logging")

	out := flag.CommandLine.Output()
	flag.Usage = func() {
		fmt.Fprintf(
			out,
			"Usage: %s [options] shell[:addr][:port] ...\n",
			os.Args[0],
		)
		fmt.Fprintln(out, "\nArguments:")
		fmt.Fprintln(
			out,
			"  shell[:[addr][:port]]  Shell name/path, optional listen address, optional port",
		)
		fmt.Fprintln(
			out,
			"                         Omitted addr/port use defaults from -l flag",
		)
		fmt.Fprintln(
			out,
			"                         Unspecified ports auto-increment for each service",
		)
		fmt.Fprintln(out, "                         Examples:")
		fmt.Fprintln(
			out,
			"                           bash                     (default addr:port)",
		)
		fmt.Fprintln(
			out,
			"                           bash:192.168.0.1         (default port)",
		)
		fmt.Fprintln(
			out,
			"                           bash::8080               (default addr)",
		)
		fmt.Fprintln(
			out,
			"                           bash:192.168.0.1:8080",
		)
		fmt.Fprintln(out, "\nOptions:")
		flag.PrintDefaults()
	}

	flag.Parse()

	args := flag.Args()
	if len(args) == 0 {
		fmt.Fprintf(
			out,
			"error: at least one shell[:[addr][:port]] argument is required\n\n",
		)
		flag.Usage()
		os.Exit(1)
	}

	// Parse default listen address and port
	defaultAddr := "127.0.0.1"
	defaultPort := 50135

	if *listen != "" {
		listenParts := strings.Split(*listen, ":")
		if len(listenParts) == 1 {
			// Just port specified
			p, err := strconv.Atoi(listenParts[0])
			if err != nil {
				fmt.Fprintf(
					out,
					"error: invalid default listen port %q: %v\n",
					listenParts[0],
					err,
				)
				os.Exit(1)
			}

			defaultPort = p
		} else if len(listenParts) == 2 {
			// Addr and port specified
			if listenParts[0] != "" {
				defaultAddr = listenParts[0]
			}

			if listenParts[1] != "" {
				p, err := strconv.Atoi(listenParts[1])
				if err != nil {
					fmt.Fprintf(out, "error: invalid default listen port %q: %v\n", listenParts[1], err)
					os.Exit(1)
				}

				defaultPort = p
			}
		} else {
			fmt.Fprintf(out, "error: invalid default listen format %q\n", *listen)
			os.Exit(1)
		}
	}

	// Parent context all goroutine contexts will derive from.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Cancelling this context via signal handler will shut down all servers.
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt)

	var w io.Writer
	if *output == "-" {
		w = os.Stdout
	} else {
		f, err := os.OpenFile(*output, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			fmt.Fprintf(out, "failed to open output file %q: %v", *output, err)
		}

		defer f.Close()

		w = f
	}

	var handler slog.Handler = slog.NewTextHandler(w, nil)
	if *jsonFlag {
		handler = slog.NewJSONHandler(w, nil)
	}

	log := slog.New(handler)

	go func() {
		<-signalChan
		log.InfoContext(ctx, "shutting down")
		cancel()
	}()

	// Create servers for each shell[:addr][:port] argument
	// Track port auto-increment offset
	portOffset := 0

	var g errgroup.Group

	for _, arg := range args {
		// Parse with current default port (auto-incremented)
		currentDefaultPort := defaultPort + portOffset

		shell, addr, port, err := parseServiceArg(
			arg,
			defaultAddr,
			currentDefaultPort,
		)
		if err != nil {
			log.ErrorContext(
				ctx,
				"invalid argument",
				slog.String("arg", arg),
				slog.String("error", err.Error()),
			)
			os.Exit(1)
		}

		// If port was unspecified (defaulted), increment offset for next service
		if port == currentDefaultPort {
			portOffset++
		}

		srv, err := srv.Make(ctx, log, shell, addr, port, expo)
		if err != nil {
			log.ErrorContext(
				ctx,
				"failed to create server",
				slog.String("error", err.Error()),
			)
			os.Exit(1)
		}

		g.Go(func() error { return srv.ListenAndServe(ctx) })
	}

	if g.Wait() != nil {
		log.ErrorContext(ctx, "exiting due to error")
	}
}
