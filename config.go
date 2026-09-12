package thresh

import (
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/goccy/go-yaml"
)

const DefaultConfigPath = "thresh.yaml"

// Config is the YAML configuration for thresh.
type Config struct {
	Root     string       `yaml:"root"`
	Assets   *bool        `yaml:"assets"`
	Update   *bool        `yaml:"update"`
	Timezone string       `yaml:"timezone"`
	Window   WindowConfig `yaml:"window"`
	Sources  []Source     `yaml:"sources"`
}

// WindowConfig describes the configured window boundary.
type WindowConfig struct {
	Daily string `yaml:"daily"`
}

// Source describes an input feed.
type Source struct {
	URL  string `yaml:"url"`
	Name string `yaml:"name,omitempty"`
}

// UnmarshalYAML accepts either a URL scalar or a source mapping.
func (s *Source) UnmarshalYAML(data []byte) error {
	var scalar string
	if err := yaml.Unmarshal(data, &scalar); err == nil {
		s.URL = scalar
		s.Name = ""
		return nil
	}

	type source Source
	var value source
	if err := yaml.Unmarshal(data, &value); err != nil {
		return fmt.Errorf("source must be a URL string or mapping: %w", err)
	}
	*s = Source(value)
	return nil
}

// Validate checks values that must be valid before runtime resolution.
func (c Config) Validate() error {
	var errs []error
	if _, err := ParseDailyTime(c.Window.Daily); err != nil {
		errs = append(errs, fmt.Errorf("window.daily: %w", err))
	}
	if len(c.Sources) == 0 {
		errs = append(errs, errors.New("sources must not be empty"))
	}
	for i, source := range c.Sources {
		if err := source.Validate(); err != nil {
			errs = append(errs, fmt.Errorf("sources[%d]: %w", i, err))
		}
	}
	return errors.Join(errs...)
}

// Validate checks that the source has an absolute HTTP(S) URL without userinfo.
func (s Source) Validate() error {
	return ValidateArticleURL(s.URL)
}

// ValidateArticleURL checks that rawURL is an absolute HTTP(S) URL without
// userinfo. Feed items are untrusted input that thresh passes to mdhq as a
// command-line argument, so values such as "--root=/tmp" or "mailto:x" must
// never reach the downstream command.
func ValidateArticleURL(rawURL string) error {
	if strings.TrimSpace(rawURL) == "" {
		return errors.New("url must not be empty")
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid url %q", safeURLDisplay(rawURL))
	}
	if !strings.EqualFold(u.Scheme, "http") && !strings.EqualFold(u.Scheme, "https") {
		return fmt.Errorf("url %q must use http or https", safeURLDisplay(rawURL))
	}
	if u.Host == "" {
		return fmt.Errorf("url %q must be absolute", safeURLDisplay(rawURL))
	}
	if u.User != nil {
		return fmt.Errorf("url %q must not contain userinfo", safeURLDisplay(rawURL))
	}
	return nil
}

func safeURLDisplay(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err == nil {
		safe := *u
		safe.User = nil
		return safe.String()
	}

	authorityStart := 0
	if strings.HasPrefix(rawURL, "//") {
		authorityStart = len("//")
	} else {
		schemeEnd := strings.Index(rawURL, "://")
		if schemeEnd < 0 {
			return rawURL
		}
		authorityStart = schemeEnd + len("://")
	}
	authorityEnd := len(rawURL)
	if offset := strings.IndexAny(rawURL[authorityStart:], "/?#"); offset >= 0 {
		authorityEnd = authorityStart + offset
	}
	at := strings.LastIndex(rawURL[authorityStart:authorityEnd], "@")
	if at < 0 {
		return rawURL
	}
	return rawURL[:authorityStart] +
		rawURL[authorityStart+at+1:authorityEnd] +
		rawURL[authorityEnd:]
}

// DailyTime is a parsed local-time daily boundary.
type DailyTime struct {
	Hour   int
	Minute int
}

// ParseDailyTime parses an exact 24-hour HH:MM value.
func ParseDailyTime(value string) (DailyTime, error) {
	if len(value) != len("HH:MM") || value[2] != ':' {
		return DailyTime{}, fmt.Errorf("%q must be in HH:MM format", value)
	}
	hour, err := strconv.Atoi(value[:2])
	if err != nil || hour < 0 || hour > 23 {
		return DailyTime{}, fmt.Errorf("%q has an invalid hour", value)
	}
	minute, err := strconv.Atoi(value[3:])
	if err != nil || minute < 0 || minute > 59 {
		return DailyTime{}, fmt.Errorf("%q has an invalid minute", value)
	}
	return DailyTime{Hour: hour, Minute: minute}, nil
}

// StringValue is a flag value that distinguishes an unset value from an
// explicitly supplied empty string.
type StringValue struct {
	value string
	set   bool
}

func (v *StringValue) Set(value string) error {
	v.value = value
	v.set = true
	return nil
}

func (v *StringValue) String() string {
	if v == nil {
		return ""
	}
	return v.value
}

// Get returns the value and whether it was explicitly set.
func (v StringValue) Get() (string, bool) {
	return v.value, v.set
}

// BoolValue is a boolean flag value that distinguishes unset from false.
type BoolValue struct {
	value bool
	set   bool
}

func (v *BoolValue) Set(value string) error {
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fmt.Errorf("invalid boolean %q: %w", value, err)
	}
	v.value = parsed
	v.set = true
	return nil
}

func (v *BoolValue) String() string {
	if v == nil || !v.value {
		return "false"
	}
	return "true"
}

// IsBoolFlag allows --assets and --update to mean true without an argument.
func (*BoolValue) IsBoolFlag() bool {
	return true
}

// Get returns the value and whether it was explicitly set.
func (v BoolValue) Get() (bool, bool) {
	return v.value, v.set
}

// CLIValues contains configuration-related command-line values.
type CLIValues struct {
	Config   StringValue
	Root     StringValue
	Assets   BoolValue
	Update   BoolValue
	Timezone StringValue
	At       StringValue
}

// RegisterFlags registers configuration-related flags on fs.
func (v *CLIValues) RegisterFlags(fs *flag.FlagSet) {
	fs.Var(&v.Config, "config", "configuration file (default thresh.yaml)")
	fs.Var(&v.Root, "root", "Markdown output root")
	fs.Var(&v.Assets, "assets", "download article assets")
	fs.Var(&v.Update, "update", "update existing articles")
	fs.Var(&v.Timezone, "timezone", "timezone used for window calculation")
	fs.Var(&v.At, "at", "select the window containing this date or time")
}

// LookupEnvFunc matches os.LookupEnv and is injectable for deterministic tests.
type LookupEnvFunc func(string) (string, bool)

// ConfigPath resolves CLI > THRESH_CONFIG > the default configuration path.
// Explicit reports whether CLI or the environment selected the path.
func (v CLIValues) ConfigPath(lookupEnv LookupEnvFunc) (path string, explicit bool, err error) {
	if lookupEnv == nil {
		lookupEnv = os.LookupEnv
	}
	if value, ok := v.Config.Get(); ok {
		if value == "" {
			return "", true, errors.New("--config must not be empty")
		}
		return value, true, nil
	}
	if value, ok := lookupEnv("THRESH_CONFIG"); ok {
		if value == "" {
			return "", true, errors.New("THRESH_CONFIG must not be empty")
		}
		return value, true, nil
	}
	return DefaultConfigPath, false, nil
}

// LoadConfig reads and validates exactly path; it does not search fallback paths.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %q: %w", path, err)
	}
	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("parse config %q: %w", path, err)
	}
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("validate config %q: %w", path, err)
	}
	return &config, nil
}

// ResolvedConfig contains values after applying CLI, environment, YAML, and
// downstream/default fallbacks.
type ResolvedConfig struct {
	ConfigPath string
	Root       string
	Assets     bool
	Update     bool
	Location   *time.Location
	Daily      DailyTime
	At         *time.Time
	Sources    []Source
}

// LoadResolvedConfig selects, loads, and resolves the configuration.
func LoadResolvedConfig(cli CLIValues, lookupEnv LookupEnvFunc) (*ResolvedConfig, error) {
	path, _, err := cli.ConfigPath(lookupEnv)
	if err != nil {
		return nil, err
	}
	config, err := LoadConfig(path)
	if err != nil {
		return nil, err
	}
	resolved, err := ResolveConfig(cli, config, lookupEnv)
	if err != nil {
		return nil, err
	}
	resolved.ConfigPath = path
	return resolved, nil
}

// ResolveConfig applies CLI > THRESH_* > YAML > downstream/default fallbacks.
func ResolveConfig(cli CLIValues, config *Config, lookupEnv LookupEnvFunc) (*ResolvedConfig, error) {
	if config == nil {
		return nil, errors.New("config must not be nil")
	}
	if lookupEnv == nil {
		lookupEnv = os.LookupEnv
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}

	root := resolveString(cli.Root, "THRESH_ROOT", config.Root, lookupEnv)
	if root == "" {
		root, _ = lookupEnv("MDHQ_ROOT")
	}
	if root == "" {
		return nil, errors.New("root is required (set --root, THRESH_ROOT, root, or MDHQ_ROOT)")
	}

	assets, err := resolveBool(cli.Assets, "THRESH_ASSETS", config.Assets, false, lookupEnv)
	if err != nil {
		return nil, err
	}
	update, err := resolveBool(cli.Update, "THRESH_UPDATE", config.Update, false, lookupEnv)
	if err != nil {
		return nil, err
	}

	timezone := resolveString(cli.Timezone, "THRESH_TIMEZONE", config.Timezone, lookupEnv)
	location := time.Local
	if timezone != "" {
		location, err = time.LoadLocation(timezone)
		if err != nil {
			return nil, fmt.Errorf("timezone %q: %w", timezone, err)
		}
	}

	daily, err := ParseDailyTime(config.Window.Daily)
	if err != nil {
		return nil, fmt.Errorf("window.daily: %w", err)
	}

	var at *time.Time
	atValue, atSet := resolveOptionalString(cli.At, "THRESH_AT", lookupEnv)
	if atSet {
		parsed, parseErr := ParseAt(atValue, location)
		if parseErr != nil {
			return nil, parseErr
		}
		at = &parsed
	}

	return &ResolvedConfig{
		Root:     root,
		Assets:   assets,
		Update:   update,
		Location: location,
		Daily:    daily,
		At:       at,
		Sources:  append([]Source(nil), config.Sources...),
	}, nil
}

func resolveString(cli StringValue, envName, yamlValue string, lookupEnv LookupEnvFunc) string {
	if value, ok := cli.Get(); ok {
		return value
	}
	if value, ok := lookupEnv(envName); ok {
		return value
	}
	return yamlValue
}

func resolveOptionalString(cli StringValue, envName string, lookupEnv LookupEnvFunc) (string, bool) {
	if value, ok := cli.Get(); ok {
		return value, true
	}
	return lookupEnv(envName)
}

func resolveBool(cli BoolValue, envName string, yamlValue *bool, fallback bool, lookupEnv LookupEnvFunc) (bool, error) {
	if value, ok := cli.Get(); ok {
		return value, nil
	}
	if value, ok := lookupEnv(envName); ok {
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return false, fmt.Errorf("%s: invalid boolean %q: %w", envName, value, err)
		}
		return parsed, nil
	}
	if yamlValue != nil {
		return *yamlValue, nil
	}
	return fallback, nil
}

// ParseAt accepts RFC3339 timestamps or YYYY-MM-DD dates in location.
func ParseAt(value string, location *time.Location) (time.Time, error) {
	if location == nil {
		location = time.Local
	}
	parsed, err := ParseWindowAt(value, location)
	if err != nil {
		return time.Time{}, fmt.Errorf("at %q: %w", value, err)
	}
	return parsed, nil
}
