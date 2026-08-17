package config

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/pflag"
)

func TestLoadUsesCLIEnvironmentFileDefaultPrecedence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := writeTestFile(path, `
data_path = "data/puqu.db"
ipp_listen = ":9000"
admin_listen = "127.0.0.1:9001"
log_level = "warn"
`); err != nil {
		t.Fatal(err)
	}
	flags := testFlags(t)
	if err := flags.Set("ipp-listen", ":9200"); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(LoadOptions{
		Path:        path,
		RequireFile: true,
		Flags:       flags,
		Environ: func() []string {
			return []string{EnvAdminListen + "=0.0.0.0:9101", EnvLogLevel + "=debug"}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.IPPListen != ":9200" || cfg.AdminListen != "0.0.0.0:9101" || cfg.LogLevel != "debug" {
		t.Fatalf("config = %+v", cfg)
	}
	if want := filepath.Join(dir, "data", "puqu.db"); cfg.DataPath != want {
		t.Fatalf("data path = %q, want %q", cfg.DataPath, want)
	}
	if !cfg.ConfigFileLoaded || !cfg.HasEphemeralOverrides() || cfg.SlogLevel() != slog.LevelDebug {
		t.Fatalf("config metadata = %+v", cfg)
	}
}

func TestLoadAllowsMissingDefaultFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.toml")
	cfg, err := Load(LoadOptions{Path: path, Environ: func() []string { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	defaults := Defaults()
	if cfg.ConfigFileLoaded || cfg.IPPListen != defaults.IPPListen || cfg.AdminListen != defaults.AdminListen || cfg.LogLevel != defaults.LogLevel {
		t.Fatalf("config = %+v", cfg)
	}
}

func TestLoadUsesConfigPathEnvironmentVariable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "custom.toml")
	if err := writeTestFile(path, `ipp_listen = ":9300"`); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(LoadOptions{Environ: func() []string { return []string{EnvConfig + "=" + path} }})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ConfigFile != path || !cfg.ConfigFileLoaded || cfg.IPPListen != ":9300" {
		t.Fatalf("config = %+v", cfg)
	}
}

func TestLoadRejectsUnknownAndMissingExplicitFiles(t *testing.T) {
	t.Run("unknown key", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.toml")
		if err := writeTestFile(path, `ipp_listen = ":8631"
typo = true
`); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(LoadOptions{Path: path, RequireFile: true, Environ: func() []string { return nil }}); err == nil {
			t.Fatal("unknown key should fail")
		}
	})

	t.Run("explicit file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "missing.toml")
		if _, err := Load(LoadOptions{Path: path, RequireFile: true, Environ: func() []string { return nil }}); err == nil {
			t.Fatal("missing explicit file should fail")
		}
	})
}

func TestConfigValidation(t *testing.T) {
	valid := Defaults()
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	remote := valid
	remote.AdminListen = "0.0.0.0:8080"
	if err := remote.Validate(); err != nil {
		t.Fatalf("configured admin listener should be accepted: %v", err)
	}
	invalid := valid
	invalid.AdminListen = "0.0.0.0"
	if err := invalid.Validate(); err == nil {
		t.Fatal("admin listener without a port should be rejected")
	}
	badLevel := valid
	badLevel.LogLevel = "verbose"
	if err := badLevel.Validate(); err == nil {
		t.Fatal("unknown log level should be rejected")
	}
}

func testFlags(t *testing.T) *pflag.FlagSet {
	t.Helper()
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	flags.String("config", "", "")
	flags.String("data", "", "")
	flags.String("ipp-listen", DefaultIPPListen, "")
	flags.String("admin-listen", DefaultAdminListen, "")
	flags.String("log-level", DefaultLogLevel, "")
	return flags
}

func writeTestFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o600)
}
