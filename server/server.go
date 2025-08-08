// Package server implements the server logic for the relay application.
package server

import (
	"context"
	"log/slog"
	"os/exec"
)

type Server map[string]Service

const Protocol = "tcp"

func Make(ctx context.Context, log *slog.Logger, port int, shells ...string) Server {
	server := make(Server)
	for i, shell := range shells {
		l := log.With(
			slog.String("shell", shell),
			slog.Int("port", port+i),
			slog.String("proto", Protocol),
		)
		path, err := exec.LookPath(shell)
		if err != nil {
			l.WarnContext(ctx, "shell unregistered", slog.String("error", err.Error()))
			continue
		}
		l.InfoContext(ctx, "shell registered", slog.String("path", path))
		server[shell] = Service{
			shell: shell,
			cmd:   path,
			port:  port + i,
			proto: Protocol,
			log:   l,
		}
	}
	return server
}
