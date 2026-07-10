package app

import (
	"path/filepath"
	"testing"

	"gopkg.in/ini.v1"
)

func TestGetShowWarningDetailsDefault(t *testing.T) {
	cTrue := &Config{
		path: filepath.Join(t.TempDir(), "config"),
		cfg:  ini.Empty(),
	}

	if got := cTrue.GetShowWarningDetails(true); !got {
		t.Fatalf("GetShowWarningDetails(true)=%v want=true", got)
	}

	cFalse := &Config{
		path: filepath.Join(t.TempDir(), "config"),
		cfg:  ini.Empty(),
	}
	if got := cFalse.GetShowWarningDetails(false); got {
		t.Fatalf("GetShowWarningDetails(false)=%v want=false", got)
	}
}

func TestSetShowWarningDetailsPersists(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config")
	c := &Config{
		path: configPath,
		cfg:  ini.Empty(),
	}

	if err := c.SetShowWarningDetails(false); err != nil {
		t.Fatalf("SetShowWarningDetails(false) error: %v", err)
	}
	if got := c.GetShowWarningDetails(true); got {
		t.Fatalf("GetShowWarningDetails(true)=%v want=false", got)
	}

	reloaded, err := ini.Load(configPath)
	if err != nil {
		t.Fatalf("reload ini error: %v", err)
	}
	if got := reloaded.Section("ui").Key("show_warning_details").MustBool(true); got {
		t.Fatalf("reloaded show_warning_details=%v want=false", got)
	}
}
