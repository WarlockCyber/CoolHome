package config

import "testing"

func TestLoadParsesAllowedOrigins(t *testing.T) {
	t.Setenv("COOLHOME_ALLOWED_ORIGINS", " https://mini.example , https://other.example/ ,, ")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := []string{"https://mini.example", "https://other.example/"}
	if len(cfg.AllowedOrigins) != len(want) {
		t.Fatalf("AllowedOrigins = %q, want %q", cfg.AllowedOrigins, want)
	}
	for i := range want {
		if cfg.AllowedOrigins[i] != want[i] {
			t.Errorf("AllowedOrigins[%d] = %q, want %q", i, cfg.AllowedOrigins[i], want[i])
		}
	}
}

func TestLoadRejectsBadOrigin(t *testing.T) {
	cases := []string{
		"mini.example",                 // no scheme
		"ftp://mini.example",           // wrong scheme
		"https://mini.example/app",     // path
		"https://mini.example/#frag",   // fragment
		"https://mini.example/?q=1",    // query
		"https://user:pw@mini.example", // credentials
		"https://",                     // no host
	}
	for _, in := range cases {
		t.Run(in, func(t *testing.T) {
			t.Setenv("COOLHOME_ALLOWED_ORIGINS", in)
			if _, err := Load(); err == nil {
				t.Fatalf("Load accepted %q, want an error", in)
			}
		})
	}
}

func TestLoadAcceptsWildcardAndPlainOrigin(t *testing.T) {
	for _, in := range []string{"*", "https://mini.example", "http://127.0.0.1:8080", "https://mini.example/"} {
		t.Run(in, func(t *testing.T) {
			t.Setenv("COOLHOME_ALLOWED_ORIGINS", in)
			if _, err := Load(); err != nil {
				t.Fatalf("Load rejected %q: %v", in, err)
			}
		})
	}
}

// The console link is rendered into the page as an href. A javascript: or data:
// URL there would be a way to run script with the page's own privileges, so the
// only accepted schemes are http and https.
func TestConsoleURLRejectsScriptSchemes(t *testing.T) {
	for _, bad := range []string{
		"javascript:alert(1)",
		"data:text/html,<script>alert(1)</script>",
		"ftp://iot.tuya.com/",
		"iot.tuya.com/cloud", // no scheme
		"/cloud",             // not absolute
		"https://",           // no host
	} {
		if err := validateConsoleURL(bad); err == nil {
			t.Errorf("%q was accepted; it must be refused", bad)
		}
	}
	for _, good := range []string{
		"https://iot.tuya.com/cloud",
		"http://localhost:8081/console",
	} {
		if err := validateConsoleURL(good); err != nil {
			t.Errorf("%q was refused: %v", good, err)
		}
	}
}

func TestConsoleURLDefaultIsTheVerifiedOne(t *testing.T) {
	t.Setenv("COOLHOME_CONSOLE_URL", "")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.ConsoleURL != "https://iot.tuya.com/cloud" {
		t.Errorf("ConsoleURL = %q, want the Tuya console", cfg.ConsoleURL)
	}
}
