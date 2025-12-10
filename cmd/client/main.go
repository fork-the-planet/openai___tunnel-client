package main

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/spf13/pflag"
	"go.uber.org/fx"
	"go.uber.org/fx/fxevent"

	"go.openai.org/api/tunnel-client/pkg/app"
	"go.openai.org/api/tunnel-client/pkg/config"
	"go.openai.org/api/tunnel-client/pkg/version"
)

func main() {
	cfg, err := config.Load(os.Args[1:], os.LookupEnv)
	if err != nil {
		if errors.Is(err, pflag.ErrHelp) {
			// --help was requested; help already printed.
			os.Exit(0)
		}
		fmt.Fprintf(os.Stderr, "configure tunnel-client: %v\n", err)
		os.Exit(1)
	}

	fxApp := app.New(cfg,
		fx.Provide(func() io.Writer { return os.Stdout }),
		fx.WithLogger(func(logger *slog.Logger, cfg *config.ControlPlaneConfig) fxevent.Logger {
			return newTunnelEventLogger(logger, cfg)
		}),
	)
	fxApp.Run()
}

type tunnelEventLogger struct {
	*fxevent.SlogLogger
	logger *slog.Logger
	cfg    *config.ControlPlaneConfig
}

func newTunnelEventLogger(logger *slog.Logger, cfg *config.ControlPlaneConfig) fxevent.Logger {
	return &tunnelEventLogger{
		SlogLogger: &fxevent.SlogLogger{Logger: logger},
		logger:     logger,
		cfg:        cfg,
	}
}

func (l *tunnelEventLogger) LogEvent(event fxevent.Event) {
	if started, ok := event.(*fxevent.Started); ok && started.Err == nil {
		tunnelURL := l.cfg.BaseURL.JoinPath("v1", "tunnel", l.cfg.TunnelID.String()).String()
		l.logger.Info("🟢 tunnel-client started",
			slog.String("tunnel_id", l.cfg.TunnelID.String()),
			slog.String("tunnel_url", tunnelURL),
			slog.String("version", version.Version),
		)
	} else {
		l.SlogLogger.LogEvent(event)
	}
}
