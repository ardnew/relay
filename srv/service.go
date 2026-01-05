package srv

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"

	"golang.org/x/sync/errgroup"
)

const Protocol = "tcp"

type Service struct {
	shell string
	cmd   string
	addr  string
	port  int
	proto string
	expo  map[string]string

	log *slog.Logger
}

func Make(
	ctx context.Context,
	log *slog.Logger,
	shell, addr string,
	port int,
	expo map[string]string,
) (*Service, error) {
	attr := []any{}
	for k, v := range expo {
		attr = append(attr, slog.String(k, v))
	}

	l := log.With(
		slog.String("shell", shell),
		slog.String("addr", addr),
		slog.Int("port", port),
		slog.String("proto", Protocol),
		slog.Group("exports", attr...),
	)

	path, err := exec.LookPath(shell)
	if err != nil {
		return nil, fmt.Errorf("shell %q not found: %w", shell, err)
	}

	l.InfoContext(ctx, "shell registered", slog.String("path", path))

	return &Service{
		shell: shell,
		cmd:   path,
		addr:  addr,
		port:  port,
		proto: Protocol,
		expo:  expo,
		log:   l,
	}, nil
}

func (s Service) ListenAndServe(ctx context.Context) error {
	ln, err := new(
		net.ListenConfig,
	).Listen(ctx, s.proto, fmt.Sprintf("%s:%d", s.addr, s.port))
	if err != nil {
		s.log.ErrorContext(
			ctx,
			"failed to listen",
			slog.String("error", err.Error()),
		)

		return err
	}
	defer ln.Close()

	go func() {
		<-ctx.Done()
		ln.Close()
	}()

	var g errgroup.Group

	s.log.InfoContext(ctx, "state changed", slog.String("state", "listening"))

	for {
		conn, err := ln.Accept()

		select {
		case <-ctx.Done():
			s.log.InfoContext(ctx, "state changed", slog.String("state", "shutdown"))

			return nil
		default:
			if err != nil {
				s.log.WarnContext(
					ctx,
					"failed to accept",
					slog.String("error", err.Error()),
				)

				continue
			}

			s.log.InfoContext(
				ctx,
				"accepted connection",
				slog.String("source", conn.RemoteAddr().String()),
			)
			g.Go(func() error {
				defer conn.Close()

				return s.HandleConnection(ctx, conn)
			})
		}
	}
}

func (s Service) HandleConnection(ctx context.Context, conn net.Conn) error {
	reader := bufio.NewReader(conn)

	for {
		eofMarker, err := reader.ReadString('\n')
		if err != nil {
			if !errors.Is(err, io.EOF) {
				fmt.Fprintf(conn, "error: failed to read EOF marker: %v\n", err)
				s.log.WarnContext(
					ctx,
					"failed to read EOF marker",
					slog.String("error", err.Error()),
				)
			}

			return nil // close connection on read error
		}

		eofMarker = eofMarker[:len(eofMarker)-1]
		if len(eofMarker) == 0 {
			continue // ignore empty marker
		}

		tmpFile, err := os.CreateTemp("", "relay-script-*")
		if err != nil {
			fmt.Fprintf(conn, "error: failed to create temp file: %v\n", err)
			s.log.ErrorContext(
				ctx,
				"failed to create temp file",
				slog.String("error", err.Error()),
			)

			return err
		}
		defer os.Remove(tmpFile.Name())

		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				if err != io.EOF {
					fmt.Fprintf(conn, "error: failed to read script: %v\n", err)
					s.log.WarnContext(
						ctx,
						"failed to read script",
						slog.String("error", err.Error()),
					)
				}

				return nil // close connection on read error
			}

			trimmed := line
			if len(trimmed) > 0 && trimmed[len(trimmed)-1] == '\n' {
				trimmed = trimmed[:len(trimmed)-1]
			}

			if trimmed == eofMarker {
				break
			}

			if _, err := tmpFile.WriteString(line); err != nil {
				fmt.Fprintf(conn, "error: failed to write script: %v\n", err)
				s.log.WarnContext(
					ctx,
					"failed to write script",
					slog.String("error", err.Error()),
				)

				return nil // close connection on write error
			}
		}

		tmpFile.Close()

		export := []string{}
		for k, v := range s.expo {
			export = append(export, fmt.Sprintf("%s=%s", k, v))
		}

		cmd := exec.CommandContext(ctx, s.cmd, "-l", tmpFile.Name())
		stdoutPipe, _ := cmd.StdoutPipe()
		stderrPipe, _ := cmd.StderrPipe()
		cmd.Env = append(cmd.Env, export...)

		if err := cmd.Start(); err != nil {
			fmt.Fprintf(conn, "error: failed to start shell: %v\n", err)
			s.log.WarnContext(
				ctx,
				"failed to start shell",
				slog.String("error", err.Error()),
			)

			return nil
		}

		stdout, _ := io.ReadAll(stdoutPipe)
		stderr, _ := io.ReadAll(stderrPipe)
		cmdErr := cmd.Wait()

		if len(stdout) > 0 {
			conn.Write(stdout)
		}

		if len(stderr) > 0 {
			conn.Write(stderr)
		}

		os.Remove(tmpFile.Name())

		if cmdErr != nil {
			fmt.Fprintf(conn, "error: script execution failed: %v\n", cmdErr)
			s.log.WarnContext(
				ctx,
				"script execution failed",
				slog.String("error", cmdErr.Error()),
			)

			continue
		}

		s.log.InfoContext(ctx, "script executed successfully")

		return nil
	}
}
