package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/WarlockCyber/CoolHome/internal/config"
	"github.com/WarlockCyber/CoolHome/internal/dashboard"
	"github.com/WarlockCyber/CoolHome/internal/demo"
	"github.com/WarlockCyber/CoolHome/internal/home"
	"github.com/WarlockCyber/CoolHome/internal/httpapi"
	"github.com/WarlockCyber/CoolHome/internal/ir"
	"github.com/WarlockCyber/CoolHome/internal/rules"
	"github.com/WarlockCyber/CoolHome/internal/scene"
	"github.com/WarlockCyber/CoolHome/internal/tuya"
)

func main() {
	if err := run(); err != nil {
		slog.Error("coolhome stopped", "err", err)
		os.Exit(1)
	}
}

func run() error {
	addr := flag.String("addr", "", "listen address (default 127.0.0.1:8080)")
	demoMode := flag.Bool("demo", false, "use fake devices, no Tuya credentials needed")
	interval := flag.Duration("interval", 0, "device polling interval (default 10s)")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if *addr != "" {
		cfg.Addr = *addr
	}
	if *demoMode {
		cfg.Demo = true
	}
	if *interval > 0 {
		cfg.PollInterval = *interval
	}
	if err := cfg.Validate(); err != nil {
		return err
	}

	var provider home.Provider
	if cfg.Demo {
		provider = demo.New()
		logger.Info("demo mode: fake devices, the Tuya cloud is not contacted")
	} else {
		client, err := tuya.New(cfg.Tuya)
		if err != nil {
			return err
		}
		provider = client
		logger.Info("tuya client ready", "base_url", client.BaseURL(), "poll_interval", cfg.PollInterval.String())
		// Optional surfaces are additive: the composite keeps the dashboard
		// working when one of them is not subscribed.
		var extra []home.Provider
		if cfg.IR {
			// With scenes on, learned RF switches are hidden: they cannot be
			// written, and a same-named scene is the working alternative.
			extra = append(extra, ir.New(client, ir.Options{
				Blasters:    cfg.IRBlasters,
				SkipLearned: cfg.Scenes,
			}, logger))
			logger.Info("infrared remotes enabled",
				"blasters", len(cfg.IRBlasters), "learned_switches_hidden", cfg.Scenes)
		}
		if cfg.Scenes {
			extra = append(extra, scene.New(client, cfg.SpaceID, logger))
			logger.Info("tap-to-run scenes enabled")
		}
		if cfg.Scenes {
			// A learned RF switch also exists as a bare device entry with no
			// writable data point. Scenes are its working path, so the bare
			// entry would show the same light twice — once broken. Sensors and
			// the blaster itself are not covered by scenes and stay visible.
			provider = home.NewFilter(provider, func(d tuya.Device) bool {
				return !strings.HasPrefix(strings.ToLower(d.Category), "rf_switch")
			})
		}
		if len(extra) > 0 {
			provider = home.NewMulti(provider, extra...)
		}
	}

	svc := home.New(provider, cfg.DeviceIDs, cfg.PollInterval, logger)

	// Rules are opt-in: an automation that sends commands to a real house should
	// be something a person turned on.
	var runner *rules.Runner
	if cfg.Rules {
		loc := time.Local
		if cfg.TZ != "" {
			l, err := time.LoadLocation(cfg.TZ)
			if err != nil {
				return err
			}
			loc = l
		}
		interval := cfg.RulesInterval
		if interval <= 0 {
			interval = cfg.PollInterval
		}
		r, err := rules.NewRunner(rules.Options{
			Store:    rules.New(cfg.RulesFile),
			Executor: svc,
			Snapshot: svc.Snapshot,
			Logger:   logger,
			Interval: interval,
			Location: loc,
		})
		if err != nil {
			return err
		}
		runner = r
		logger.Info("rules enabled",
			"file", cfg.RulesFile, "interval", interval.String(), "tz", loc.String())
		// A schedule with only "from" is a one-minute window. An evaluation
		// interval as long as that window can step over it entirely, and the rule
		// would never fire — silently, once a day.
		if set, err := r.Store().Get(); err == nil {
			minutes := 0
			for _, rule := range set.Rules {
				if rule.At != nil && rule.At.To == "" {
					minutes++
				}
			}
			if minutes > 0 && interval >= time.Minute {
				logger.Warn("rules: evaluation interval is a minute or longer, "+
					"so \"at HH:MM\" rules can be stepped over; set COOLHOME_RULES_INTERVAL below 60s "+
					"or give those rules a window (from/to)",
					"interval", interval.String(), "rules_with_a_one_minute_window", minutes)
			}
		}
	}

	handler, err := httpapi.New(svc, httpapi.Options{
		AuthToken:      cfg.AuthToken,
		Demo:           cfg.Demo,
		Logger:         logger,
		AllowedOrigins: cfg.AllowedOrigins,
		IR:             cfg.IR && !cfg.Demo,
		Dashboard:      dashboard.New(cfg.DashboardFile),
		ConsoleURL:     cfg.ConsoleURL,
		Rules:          runner,
		RulesLocation:  rulesLocation(cfg.TZ),
	})
	if err != nil {
		return err
	}

	// Bind before anything else touches the network. Binding late meant a busy
	// port surfaced as "listening" followed by a bind error and a cancelled
	// first refresh, which read like a Tuya fault rather than a taken port.
	ln, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		if isAddrInUse(err) {
			return fmt.Errorf("%s is already in use — another CoolHome is probably running; "+
				"stop it or choose another COOLHOME_ADDR: %w", cfg.Addr, err)
		}
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go svc.Run(ctx)
	if runner != nil {
		go runner.Run(ctx)
	}

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       90 * time.Second,
	}

	if cfg.AuthToken == "" {
		logger.Warn("API is unprotected: keep the listener on localhost or set COOLHOME_AUTH_TOKEN")
	}
	if slices.Contains(cfg.AllowedOrigins, "*") {
		logger.Warn("CORS is open to any origin: any web page can call the API with a token it has seen")
	}

	errCh := make(chan error, 1)
	go func() {
		// The address is already bound, so this line is now true when written.
		logger.Info("coolhome listening", "addr", ln.Addr().String(), "demo", cfg.Demo)
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	logger.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

// isAddrInUse reports a port already taken, which has an obvious fix and
// deserves a hint rather than a bare syscall error.
func isAddrInUse(err error) bool {
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		var sysErr *os.SyscallError
		if errors.As(opErr.Err, &sysErr) {
			return errors.Is(sysErr.Err, syscall.EADDRINUSE)
		}
	}
	return errors.Is(err, syscall.EADDRINUSE)
}

// rulesLocation resolves the zone rule schedules are read in, for the API so a
// rule preview and the runner agree about what "22:00" means.
func rulesLocation(name string) *time.Location {
	if name == "" {
		return time.Local
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return time.Local
	}
	return loc
}
