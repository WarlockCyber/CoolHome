// Package config loads runtime configuration from the environment.
package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/WarlockCyber/CoolHome/internal/tuya"
)

// Config is the full application configuration.
type Config struct {
	Addr         string
	Demo         bool
	AuthToken    string
	PollInterval time.Duration
	DeviceIDs    []string
	// AllowedOrigins enables CORS for browser clients such as a Tuya mini-app.
	AllowedOrigins []string
	// IR enables the infrared provider: remotes stored on a blaster appear as
	// devices whose functions are buttons. Requires the IR Control service to
	// be subscribed in the Tuya console.
	IR bool
	// IRBlasters pins the blaster device ids. Empty means discover them.
	IRBlasters []string
	// Scenes enables the scene provider: tap-to-run scenes appear as devices
	// with one button. This is the only supported way to run an action the
	// data-point API cannot perform, such as a learned RF key.
	Scenes bool
	// SpaceID pins the home/space the scene API is addressed by. Empty means
	// discover it from the account's devices.
	SpaceID string
	// DashboardFile persists which controls the remote shows. Empty keeps the
	// layout in memory only.
	DashboardFile string
	// ConsoleURL is where a message tells the user to go when the cloud refuses
	// for a reason only the Tuya console can fix — an exhausted device quota, a
	// service that is not subscribed. It is configurable because the console is
	// not ours: if Tuya moves it, the message moves with one environment
	// variable instead of a release.
	ConsoleURL string
	// Rules enables the automation engine: "condition → action" rules stored in
	// RulesFile. Off by default, because an automation that sends commands to a
	// real house should be something a person turned on.
	Rules bool
	// RulesFile stores the rules. Empty keeps them in memory only.
	RulesFile string
	// RulesInterval is how often rules are evaluated. Empty means the poll
	// interval: a rule cannot see a change its input has not seen.
	RulesInterval time.Duration
	// TZ is the zone rule schedules are read in. Empty means the machine's
	// local zone. It is an IANA name ("Europe/Moscow"), not an offset, so
	// daylight saving is handled by the zone database rather than by arithmetic.
	TZ   string
	Tuya tuya.Config
}

// Load reads configuration from the environment, applying defaults.
func Load() (Config, error) {
	get := os.Getenv

	cfg := Config{
		Addr:           strings.TrimSpace(get("COOLHOME_ADDR")),
		AuthToken:      get("COOLHOME_AUTH_TOKEN"),
		DeviceIDs:      splitList(get("COOLHOME_DEVICE_IDS")),
		AllowedOrigins: splitList(get("COOLHOME_ALLOWED_ORIGINS")),
	}
	if cfg.Addr == "" {
		cfg.Addr = "127.0.0.1:8080"
	}

	if raw := strings.TrimSpace(get("COOLHOME_IR")); raw != "" {
		enabled, err := strconv.ParseBool(raw)
		if err != nil {
			return Config{}, fmt.Errorf("config: COOLHOME_IR=%q is not a boolean", raw)
		}
		cfg.IR = enabled
	}
	cfg.IRBlasters = splitList(get("COOLHOME_IR_BLASTERS"))
	cfg.SpaceID = strings.TrimSpace(get("COOLHOME_SPACE_ID"))
	cfg.DashboardFile = strings.TrimSpace(get("COOLHOME_DASHBOARD_FILE"))
	cfg.ConsoleURL = strings.TrimSpace(get("COOLHOME_CONSOLE_URL"))
	if cfg.ConsoleURL == "" {
		// Verified reachable (HTTP 200) at the time of writing. It is the list of
		// Cloud projects, where a project's plan and its device quota are shown;
		// there is no stable public URL for the upgrade page itself, so linking
		// to a guess would be worse than linking to the console.
		cfg.ConsoleURL = defaultConsoleURL
	}
	if err := validateConsoleURL(cfg.ConsoleURL); err != nil {
		return Config{}, fmt.Errorf("config: COOLHOME_CONSOLE_URL: %w", err)
	}

	if raw := strings.TrimSpace(get("COOLHOME_SCENES")); raw != "" {
		enabled, err := strconv.ParseBool(raw)
		if err != nil {
			return Config{}, fmt.Errorf("config: COOLHOME_SCENES=%q is not a boolean", raw)
		}
		cfg.Scenes = enabled
	}

	if raw := strings.TrimSpace(get("COOLHOME_RULES")); raw != "" {
		enabled, err := strconv.ParseBool(raw)
		if err != nil {
			return Config{}, fmt.Errorf("config: COOLHOME_RULES=%q is not a boolean", raw)
		}
		cfg.Rules = enabled
	}
	cfg.RulesFile = strings.TrimSpace(get("COOLHOME_RULES_FILE"))
	if cfg.RulesFile != "" {
		// A file implies the feature: asking for rules to be persisted and then
		// not running them would be a surprising reading of the setting.
		cfg.Rules = true
	}
	cfg.TZ = strings.TrimSpace(get("COOLHOME_TZ"))
	if cfg.TZ != "" {
		if _, err := time.LoadLocation(cfg.TZ); err != nil {
			return Config{}, fmt.Errorf("config: COOLHOME_TZ=%q is not a known time zone (%w)", cfg.TZ, err)
		}
	}
	if raw := strings.TrimSpace(get("COOLHOME_RULES_INTERVAL")); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil {
			return Config{}, fmt.Errorf("config: COOLHOME_RULES_INTERVAL=%q: %w", raw, err)
		}
		if d < time.Second {
			return Config{}, fmt.Errorf("config: COOLHOME_RULES_INTERVAL must be at least 1s")
		}
		cfg.RulesInterval = d
	}

	if raw := strings.TrimSpace(get("COOLHOME_DEMO")); raw != "" {
		demo, err := strconv.ParseBool(raw)
		if err != nil {
			return Config{}, fmt.Errorf("config: COOLHOME_DEMO=%q is not a boolean", raw)
		}
		cfg.Demo = demo
	}

	cfg.PollInterval = 10 * time.Second
	if raw := strings.TrimSpace(get("COOLHOME_POLL_INTERVAL")); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil {
			return Config{}, fmt.Errorf("config: COOLHOME_POLL_INTERVAL=%q: %w", raw, err)
		}
		if d < time.Second {
			return Config{}, fmt.Errorf("config: COOLHOME_POLL_INTERVAL must be at least 1s")
		}
		cfg.PollInterval = d
	}

	region := strings.TrimSpace(get("TUYA_REGION"))
	if region == "" {
		region = "eu"
	}
	cfg.Tuya = tuya.Config{
		ClientID: strings.TrimSpace(get("TUYA_CLIENT_ID")),
		Secret:   strings.TrimSpace(get("TUYA_CLIENT_SECRET")),
		Region:   region,
		BaseURL:  strings.TrimSpace(get("TUYA_BASE_URL")),
		UID:      strings.TrimSpace(get("TUYA_UID")),
	}

	for _, origin := range cfg.AllowedOrigins {
		if err := validateOrigin(origin); err != nil {
			return Config{}, fmt.Errorf("config: COOLHOME_ALLOWED_ORIGINS: %w", err)
		}
	}

	return cfg, nil
}

// defaultConsoleURL is the Tuya Cloud console. Only http(s) is accepted for the
// configured value: the link is rendered into the page, and a javascript: or
// data: URL there would be a way to run script in it.
const defaultConsoleURL = "https://iot.tuya.com/cloud"

// validateConsoleURL accepts an absolute http(s) URL, which is what a message
// can safely link to.
func validateConsoleURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%q is not a URL: %w", raw, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("%q must be an http(s) URL", raw)
	}
	if u.Host == "" {
		return fmt.Errorf("%q has no host", raw)
	}
	return nil
}

// validateOrigin accepts "*" or an absolute http(s) origin with no path, so a
// pasted URL with a route is caught at startup instead of silently never
// matching the Origin header the browser sends.
func validateOrigin(origin string) error {
	if origin == "*" {
		return nil
	}
	u, err := url.Parse(origin)
	if err != nil {
		return fmt.Errorf("%q is not a valid origin: %w", origin, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("%q must start with http:// or https://", origin)
	}
	if u.Host == "" {
		return fmt.Errorf("%q has no host", origin)
	}
	if u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("%q must be an origin only, without path, query or credentials (want e.g. https://example.com)", origin)
	}
	return nil
}

// Validate checks the configuration after command-line overrides are applied.
func (c Config) Validate() error {
	if c.Demo {
		return nil
	}
	if c.Tuya.ClientID == "" || c.Tuya.Secret == "" {
		return fmt.Errorf("config: TUYA_CLIENT_ID and TUYA_CLIENT_SECRET are required (or run with -demo / COOLHOME_DEMO=true)")
	}
	return nil
}

func splitList(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var out []string
	for _, part := range strings.Split(raw, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}
