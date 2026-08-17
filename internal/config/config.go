// Package config owns immutable process bootstrap configuration.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/knadh/koanf/parsers/toml/v2"
	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/providers/env/v2"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/providers/posflag"
	"github.com/knadh/koanf/v2"
	"github.com/spf13/pflag"
)

const (
	DefaultIPPListen   = ":8631"
	DefaultAdminListen = "127.0.0.1:8080"
	DefaultLogLevel    = "info"

	EnvConfig      = "PUQU_CONFIG"
	EnvDataPath    = "PUQU_DATA_PATH"
	EnvIPPListen   = "PUQU_IPP_LISTEN"
	EnvAdminListen = "PUQU_ADMIN_LISTEN"
	EnvLogLevel    = "PUQU_LOG_LEVEL"
)

var keys = map[string]struct{}{
	"data_path":    {},
	"ipp_listen":   {},
	"admin_listen": {},
	"log_level":    {},
}

// Config is the fully resolved startup configuration. Mutable printer and job
// state belongs in SQLite, not here.
type Config struct {
	ConfigFile       string `json:"configFile" koanf:"-"`
	ConfigFileLoaded bool   `json:"configFileLoaded" koanf:"-"`
	DataPath         string `json:"dataPath" koanf:"data_path"`
	IPPListen        string `json:"ippListen" koanf:"ipp_listen"`
	AdminListen      string `json:"adminListen" koanf:"admin_listen"`
	LogLevel         string `json:"logLevel" koanf:"log_level"`

	ephemeralOverrides bool
}

// LoadOptions supplies the config path and Cobra's parsed persistent flags.
// Environ is injectable so precedence can be tested without process globals.
type LoadOptions struct {
	Path        string
	RequireFile bool
	Flags       *pflag.FlagSet
	Environ     func() []string
}

func Defaults() Config {
	return Config{
		IPPListen:   DefaultIPPListen,
		AdminListen: DefaultAdminListen,
		LogLevel:    DefaultLogLevel,
	}
}

func DefaultFile() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "puqu-ipp", "config.toml"), nil
}

// Load merges defaults, TOML, environment variables, then explicitly changed
// CLI flags. A missing default file is allowed; an explicit file is required.
func Load(options LoadOptions) (Config, error) {
	configPath, requireFile, err := configFile(options)
	if err != nil {
		return Config{}, err
	}

	k := koanf.New(".")
	defaults := Defaults()
	if err := k.Load(confmap.Provider(map[string]any{
		"data_path":    defaults.DataPath,
		"ipp_listen":   defaults.IPPListen,
		"admin_listen": defaults.AdminListen,
		"log_level":    defaults.LogLevel,
	}, "."), nil); err != nil {
		return Config{}, fmt.Errorf("load config defaults: %w", err)
	}

	loaded := false
	if _, err := os.Stat(configPath); err == nil {
		if err := k.Load(file.Provider(configPath), toml.Parser()); err != nil {
			return Config{}, fmt.Errorf("load config file %s: %w", configPath, err)
		}
		loaded = true
	} else if !errors.Is(err, os.ErrNotExist) || requireFile {
		return Config{}, fmt.Errorf("load config file %s: %w", configPath, err)
	}

	ephemeral := false
	dataOverridden := false
	environ := options.Environ
	if environ == nil {
		environ = os.Environ
	}
	if err := k.Load(env.Provider(".", env.Opt{
		Prefix:      "PUQU_",
		EnvironFunc: environ,
		TransformFunc: func(name, value string) (string, any) {
			key := envKey(name)
			if key != "" {
				ephemeral = true
				dataOverridden = dataOverridden || key == "data_path"
			}
			return key, value
		},
	}), nil); err != nil {
		return Config{}, fmt.Errorf("load environment config: %w", err)
	}

	if options.Flags != nil {
		provider := posflag.ProviderWithFlag(options.Flags, ".", k, func(flag *pflag.Flag) (string, any) {
			key := flagKey(flag.Name)
			if key == "" {
				return "", nil
			}
			if flag.Changed {
				ephemeral = true
				dataOverridden = dataOverridden || key == "data_path"
			}
			return key, posflag.FlagVal(options.Flags, flag)
		})
		if err := k.Load(provider, nil); err != nil {
			return Config{}, fmt.Errorf("load command-line config: %w", err)
		}
	}

	for _, key := range k.Keys() {
		if _, ok := keys[key]; !ok {
			return Config{}, fmt.Errorf("unknown config key %q", key)
		}
	}

	var result Config
	if err := k.Unmarshal("", &result); err != nil {
		return Config{}, fmt.Errorf("decode config: %w", err)
	}
	result.ConfigFile = configPath
	result.ConfigFileLoaded = loaded
	result.ephemeralOverrides = ephemeral
	result.DataPath = strings.TrimSpace(result.DataPath)
	result.IPPListen = strings.TrimSpace(result.IPPListen)
	result.AdminListen = strings.TrimSpace(result.AdminListen)
	result.LogLevel = strings.ToLower(strings.TrimSpace(result.LogLevel))
	if result.DataPath != "" && !filepath.IsAbs(result.DataPath) {
		base := ""
		if loaded && !dataOverridden {
			base = filepath.Dir(configPath)
		}
		result.DataPath, err = filepath.Abs(filepath.Join(base, result.DataPath))
		if err != nil {
			return Config{}, fmt.Errorf("resolve data path: %w", err)
		}
	}
	if err := result.Validate(); err != nil {
		return Config{}, err
	}
	return result, nil
}

func (c Config) Validate() error {
	if c.IPPListen == "" || c.AdminListen == "" {
		return errors.New("listen addresses are required")
	}
	if err := validateListenAddress(c.IPPListen); err != nil {
		return fmt.Errorf("invalid IPP listen address: %w", err)
	}
	if err := validateListenAddress(c.AdminListen); err != nil {
		return fmt.Errorf("invalid admin listen address: %w", err)
	}
	var level slog.Level
	if err := level.UnmarshalText([]byte(c.LogLevel)); err != nil {
		return fmt.Errorf("invalid log level %q: use debug, info, warn, or error", c.LogLevel)
	}
	return nil
}

func (c Config) SlogLevel() slog.Level {
	var level slog.Level
	_ = level.UnmarshalText([]byte(c.LogLevel))
	return level
}

func (c Config) HasEphemeralOverrides() bool { return c.ephemeralOverrides }

func configFile(options LoadOptions) (string, bool, error) {
	path := strings.TrimSpace(options.Path)
	required := options.RequireFile
	if path == "" {
		if value, ok := lookupEnv(options.Environ, EnvConfig); ok && strings.TrimSpace(value) != "" {
			path = strings.TrimSpace(value)
			required = true
		}
	}
	if path == "" {
		var err error
		path, err = DefaultFile()
		if err != nil {
			return "", false, fmt.Errorf("resolve default config file: %w", err)
		}
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return "", false, fmt.Errorf("resolve config file: %w", err)
	}
	return filepath.Clean(path), required, nil
}

func lookupEnv(environ func() []string, name string) (string, bool) {
	if environ == nil {
		environ = os.Environ
	}
	for _, item := range environ() {
		key, value, ok := strings.Cut(item, "=")
		if ok && key == name {
			return value, true
		}
	}
	return "", false
}

func envKey(name string) string {
	switch name {
	case EnvDataPath:
		return "data_path"
	case EnvIPPListen:
		return "ipp_listen"
	case EnvAdminListen:
		return "admin_listen"
	case EnvLogLevel:
		return "log_level"
	default:
		return ""
	}
}

func flagKey(name string) string {
	switch name {
	case "data":
		return "data_path"
	case "ipp-listen":
		return "ipp_listen"
	case "admin-listen":
		return "admin_listen"
	case "log-level":
		return "log_level"
	default:
		return ""
	}
}

func validateListenAddress(address string) error {
	_, port, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return errors.New("port must be between 1 and 65535")
	}
	return nil
}
