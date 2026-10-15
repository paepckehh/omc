package notify

import (
	"testing"
	"time"
)

func TestNormalizeMode(t *testing.T) {
	cases := map[string]string{
		"":       "auto",
		"auto":   "auto",
		"AUTO":   "auto",
		"junk":   "auto",
		" on ":   "on",
		"ON":     "on",
		"1":      "on",
		"true":   "on",
		"yes":    "on",
		"force":  "on",
		"off":    "off",
		"OFF":    "off",
		"0":      "off",
		"false":  "off",
		"no":     "off",
		"enable": "auto",
	}
	for raw, want := range cases {
		if got := NormalizeMode(raw); got != want {
			t.Errorf("NormalizeMode(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestDesktop(t *testing.T) {
	cases := map[string]string{
		"":                     "",
		"GNOME":                "GNOME",
		"GNOME:ubuntu:wayland": "GNOME",
		"ubuntu:GNOME":         "ubuntu",
		"KDE":                  "KDE",
		"  LXQt  ":             "LXQt",
	}
	for raw, want := range cases {
		t.Setenv("XDG_CURRENT_DESKTOP", raw)
		if got := Desktop(); got != want {
			t.Errorf("Desktop(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestDetectOff(t *testing.T) {
	if n := Detect("off"); n != nil {
		t.Errorf("Detect(off) must be nil, got %#v", n)
	}
	if n := Detect("OFF"); n != nil {
		t.Errorf("Detect(OFF) must be nil, got %#v", n)
	}
}

func TestDetectNeverBlocksOrPanics(t *testing.T) {
	// Whatever the runtime environment (session bus reachable or not,
	// notification daemon running or not), Detect must return quickly and
	// without panicking; "auto" performs the full detection, "on" skips
	// the daemon probe.
	for _, mode := range []string{"auto", "on", ""} {
		start := time.Now()
		n := Detect(mode)
		if elapsed := time.Since(start); elapsed > 5*time.Second {
			t.Errorf("Detect(%q) blocked for %s", mode, elapsed)
		}
		// A non-nil result must describe itself for diagnostics.
		if n != nil && n.Describe() == "" {
			t.Errorf("Detect(%q) returned a notifier without a description", mode)
		}
	}
}
