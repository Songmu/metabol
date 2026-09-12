package thresh

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/goccy/go-yaml"
)

func boolPointer(value bool) *bool {
	return &value
}

func lookup(values map[string]string) LookupEnvFunc {
	return func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	}
}

func validConfig() *Config {
	return &Config{
		Root:     "yaml-root",
		Assets:   boolPointer(true),
		Update:   boolPointer(true),
		Timezone: "UTC",
		Window:   WindowConfig{Daily: "07:30"},
		Sources:  []Source{{URL: "https://example.com/feed"}},
	}
}

func TestSourceUnmarshalYAML(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		want Source
	}{
		{
			name: "scalar",
			yaml: "https://example.com/feed",
			want: Source{URL: "https://example.com/feed"},
		},
		{
			name: "mapping",
			yaml: "url: https://example.com/feed\nname: example",
			want: Source{URL: "https://example.com/feed", Name: "example"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got Source
			if err := yaml.Unmarshal([]byte(tt.yaml), &got); err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("got %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr string
	}{
		{name: "valid", mutate: func(*Config) {}},
		{name: "no sources", mutate: func(c *Config) { c.Sources = nil }, wantErr: "sources must not be empty"},
		{name: "relative source", mutate: func(c *Config) { c.Sources[0].URL = "/feed" }, wantErr: "must use http or https"},
		{name: "source userinfo", mutate: func(c *Config) { c.Sources[0].URL = "https://user@example.com/feed" }, wantErr: "must not contain userinfo"},
		{name: "invalid daily", mutate: func(c *Config) { c.Window.Daily = "7:30" }, wantErr: "HH:MM"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := validConfig()
			tt.mutate(config)
			err := config.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("got error %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestParseDailyTime(t *testing.T) {
	tests := []struct {
		value   string
		want    DailyTime
		wantErr bool
	}{
		{value: "00:00", want: DailyTime{Hour: 0, Minute: 0}},
		{value: "23:59", want: DailyTime{Hour: 23, Minute: 59}},
		{value: "24:00", wantErr: true},
		{value: "12:60", wantErr: true},
		{value: "7:00", wantErr: true},
		{value: "07:00:00", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			got, err := ParseDailyTime(tt.value)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseDailyTime(%q) error = %v, wantErr %v", tt.value, err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("ParseDailyTime(%q) = %#v, want %#v", tt.value, got, tt.want)
			}
		})
	}
}

func TestCLIValuesRegisterFlags(t *testing.T) {
	var values CLIValues
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	values.RegisterFlags(fs)
	if err := fs.Parse([]string{
		"--config=custom.yaml",
		"--root=",
		"--assets=false",
		"--update",
		"--timezone=Asia/Tokyo",
		"--at=2026-09-11",
	}); err != nil {
		t.Fatal(err)
	}

	if value, set := values.Config.Get(); value != "custom.yaml" || !set {
		t.Fatalf("config = %q, %v", value, set)
	}
	if value, set := values.Root.Get(); value != "" || !set {
		t.Fatalf("root = %q, %v", value, set)
	}
	if value, set := values.Assets.Get(); value || !set {
		t.Fatalf("assets = %v, %v", value, set)
	}
	if value, set := values.Update.Get(); !value || !set {
		t.Fatalf("update = %v, %v", value, set)
	}
}

func TestCLIValuesConfigPath(t *testing.T) {
	tests := []struct {
		name         string
		cli          string
		env          map[string]string
		want         string
		wantExplicit bool
		wantErr      bool
	}{
		{name: "default", want: DefaultConfigPath},
		{name: "environment", env: map[string]string{"THRESH_CONFIG": "env.yaml"}, want: "env.yaml", wantExplicit: true},
		{name: "cli", cli: "cli.yaml", env: map[string]string{"THRESH_CONFIG": "env.yaml"}, want: "cli.yaml", wantExplicit: true},
		{name: "empty environment", env: map[string]string{"THRESH_CONFIG": ""}, wantExplicit: true, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var values CLIValues
			if tt.cli != "" {
				if err := values.Config.Set(tt.cli); err != nil {
					t.Fatal(err)
				}
			}
			got, explicit, err := values.ConfigPath(lookup(tt.env))
			if (err != nil) != tt.wantErr {
				t.Fatalf("ConfigPath error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want || explicit != tt.wantExplicit {
				t.Fatalf("ConfigPath = %q, %v, want %q, %v", got, explicit, tt.want, tt.wantExplicit)
			}
		})
	}
}

func TestLoadConfigUsesExactPath(t *testing.T) {
	dir := t.TempDir()
	defaultPath := filepath.Join(dir, DefaultConfigPath)
	explicitPath := filepath.Join(dir, "custom.yaml")
	configYAML := "root: data\nwindow:\n  daily: \"07:00\"\nsources:\n  - https://example.com/feed\n"
	if err := os.WriteFile(defaultPath, []byte("invalid: ["), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(explicitPath, []byte(configYAML), 0o600); err != nil {
		t.Fatal(err)
	}

	config, err := LoadConfig(explicitPath)
	if err != nil {
		t.Fatal(err)
	}
	if config.Root != "data" {
		t.Fatalf("root = %q, want data", config.Root)
	}
}

func TestResolveConfigPrecedence(t *testing.T) {
	tests := []struct {
		name       string
		config     *Config
		env        map[string]string
		setCLI     func(*CLIValues)
		wantRoot   string
		wantAssets bool
		wantUpdate bool
		wantZone   string
	}{
		{
			name:       "yaml",
			config:     validConfig(),
			wantRoot:   "yaml-root",
			wantAssets: true,
			wantUpdate: true,
			wantZone:   "UTC",
		},
		{
			name:   "environment over yaml",
			config: validConfig(),
			env: map[string]string{
				"THRESH_ROOT":     "env-root",
				"THRESH_ASSETS":   "false",
				"THRESH_UPDATE":   "false",
				"THRESH_TIMEZONE": "Asia/Tokyo",
			},
			wantRoot:   "env-root",
			wantAssets: false,
			wantUpdate: false,
			wantZone:   "Asia/Tokyo",
		},
		{
			name:   "cli over environment",
			config: validConfig(),
			env: map[string]string{
				"THRESH_ROOT":     "env-root",
				"THRESH_ASSETS":   "true",
				"THRESH_UPDATE":   "true",
				"THRESH_TIMEZONE": "UTC",
			},
			setCLI: func(values *CLIValues) {
				_ = values.Root.Set("cli-root")
				_ = values.Assets.Set("false")
				_ = values.Update.Set("false")
				_ = values.Timezone.Set("Asia/Tokyo")
			},
			wantRoot:   "cli-root",
			wantAssets: false,
			wantUpdate: false,
			wantZone:   "Asia/Tokyo",
		},
		{
			name: "environment overrides invalid yaml timezone",
			config: func() *Config {
				config := validConfig()
				config.Timezone = "Not/A_Zone"
				return config
			}(),
			env:        map[string]string{"THRESH_TIMEZONE": "UTC"},
			wantRoot:   "yaml-root",
			wantAssets: true,
			wantUpdate: true,
			wantZone:   "UTC",
		},
		{
			name: "downstream and defaults",
			config: func() *Config {
				config := validConfig()
				config.Root = ""
				config.Assets = nil
				config.Update = nil
				config.Timezone = ""
				return config
			}(),
			env:        map[string]string{"MDHQ_ROOT": "mdhq-root"},
			wantRoot:   "mdhq-root",
			wantAssets: false,
			wantUpdate: false,
			wantZone:   time.Local.String(),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cli CLIValues
			if tt.setCLI != nil {
				tt.setCLI(&cli)
			}
			got, err := ResolveConfig(cli, tt.config, lookup(tt.env))
			if err != nil {
				t.Fatal(err)
			}
			if got.Root != tt.wantRoot || got.Assets != tt.wantAssets || got.Update != tt.wantUpdate {
				t.Fatalf("got root/assets/update %q/%v/%v, want %q/%v/%v",
					got.Root, got.Assets, got.Update, tt.wantRoot, tt.wantAssets, tt.wantUpdate)
			}
			if got.Location.String() != tt.wantZone {
				t.Fatalf("location = %q, want %q", got.Location, tt.wantZone)
			}
		})
	}
}

func TestResolveConfigErrors(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		env     map[string]string
		wantErr string
	}{
		{
			name: "missing root",
			config: func() *Config {
				config := validConfig()
				config.Root = ""
				return config
			}(),
			wantErr: "root is required",
		},
		{name: "invalid assets environment", config: validConfig(), env: map[string]string{"THRESH_ASSETS": "sometimes"}, wantErr: "THRESH_ASSETS"},
		{name: "invalid update environment", config: validConfig(), env: map[string]string{"THRESH_UPDATE": "sometimes"}, wantErr: "THRESH_UPDATE"},
		{
			name: "invalid yaml timezone",
			config: func() *Config {
				config := validConfig()
				config.Timezone = "Not/A_Zone"
				return config
			}(),
			wantErr: "timezone",
		},
		{name: "invalid timezone environment", config: validConfig(), env: map[string]string{"THRESH_TIMEZONE": "Not/A_Zone"}, wantErr: "timezone"},
		{name: "invalid at environment", config: validConfig(), env: map[string]string{"THRESH_AT": "last-week"}, wantErr: "must be RFC3339 or YYYY-MM-DD"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ResolveConfig(CLIValues{}, tt.config, lookup(tt.env))
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("got error %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestResolveConfigAt(t *testing.T) {
	tests := []struct {
		name string
		at   string
		zone string
		want string
	}{
		{name: "date in configured timezone", at: "2026-09-11", zone: "Asia/Tokyo", want: "2026-09-11T00:00:00+09:00"},
		{name: "RFC3339 keeps offset", at: "2026-09-11T12:34:56-04:00", zone: "Asia/Tokyo", want: "2026-09-11T12:34:56-04:00"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := validConfig()
			config.Timezone = tt.zone
			got, err := ResolveConfig(CLIValues{}, config, lookup(map[string]string{"THRESH_AT": tt.at}))
			if err != nil {
				t.Fatal(err)
			}
			if got.At == nil || got.At.Format(time.RFC3339) != tt.want {
				t.Fatalf("at = %v, want %s", got.At, tt.want)
			}
		})
	}
}
