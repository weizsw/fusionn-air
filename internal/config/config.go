package config

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/spf13/viper"
	"go.uber.org/zap"

	"github.com/fusionn-air/pkg/logger"
)

var log = logger.Component("config")

type Config struct {
	LogLevel  string          `mapstructure:"log_level"`
	Server    ServerConfig    `mapstructure:"server"`
	Trakt     TraktConfig     `mapstructure:"trakt"`
	Scrob     ScrobConfig     `mapstructure:"scrob"`
	Overseerr OverseerrConfig `mapstructure:"overseerr"`
	Scheduler SchedulerConfig `mapstructure:"scheduler"`
	Watcher   WatcherConfig   `mapstructure:"watcher"`
	Apprise   AppriseConfig   `mapstructure:"apprise"`
}

type ServerConfig struct {
	Port int `mapstructure:"port"`
}

type TraktConfig struct {
	ClientID     string `mapstructure:"client_id"`
	ClientSecret string `mapstructure:"client_secret"`
	BaseURL      string `mapstructure:"base_url"`
}

type ScrobConfig struct {
	BaseURL string `mapstructure:"base_url"`
	APIKey  string `mapstructure:"api_key"`
}

type OverseerrConfig struct {
	BaseURL string `mapstructure:"base_url"`
	APIKey  string `mapstructure:"api_key"`
	UserID  int    `mapstructure:"user_id"` // Request as specific user (0 = API key owner)
}

type SchedulerConfig struct {
	Cron       string `mapstructure:"cron"`
	DryRun     bool   `mapstructure:"dry_run"`
	RunOnStart bool   `mapstructure:"run_on_start"`
}

type WatcherConfig struct {
	Enabled          bool          `mapstructure:"enabled"`
	Source           string        `mapstructure:"source"`
	CalendarDays     int           `mapstructure:"calendar_days"`
	ExcludedGenres   []string      `mapstructure:"excluded_genres"`
	AllowedLanguages []string      `mapstructure:"allowed_languages"`
	Routing          RoutingConfig `mapstructure:"routing"`
}

type RoutingConfig struct {
	DefaultServerID    int      `mapstructure:"default_server_id"`
	AlternateServerID  int      `mapstructure:"alternate_server_id"`
	AlternateGenres    []string `mapstructure:"alternate_genres"`
	AlternateCountries []string `mapstructure:"alternate_countries"`
}

type AppriseConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	BaseURL string `mapstructure:"base_url"` // Apprise API URL (e.g., http://apprise:8000)
	Key     string `mapstructure:"key"`      // Apprise config key (default: apprise)
	Tag     string `mapstructure:"tag"`      // Tag to filter services (default: all)
}

// Manager owns loading and immutable snapshots of hot-reloadable configuration.
// Credentials, enabled flags, server.port, and scheduler.cron remain restart-only
// because their adapters are created by the composition root.
type Manager struct {
	mu       sync.RWMutex
	reloadMu sync.Mutex
	cfg      *Config
	viper    *viper.Viper
	stop     chan struct{}
	stopOnce sync.Once

	path        string
	lastModTime time.Time
}

func NewManager(path string) (*Manager, error) {
	v, err := configuredViper(path)
	if err != nil {
		return nil, err
	}
	cfg, err := load(v)
	if err != nil {
		return nil, err
	}

	var lastMod time.Time
	if stat, err := os.Stat(path); err == nil {
		lastMod = stat.ModTime()
	}
	m := &Manager{cfg: cfg, viper: v, stop: make(chan struct{}), path: path, lastModTime: lastMod}
	logger.SetConfigLevel(cfg.LogLevel)
	log.Info(context.Background(), "config_loaded", "Configuration loaded", zap.String("path", path))
	go m.pollForChanges(10 * time.Second)
	return m, nil
}

// Get returns an isolated snapshot.
func (m *Manager) Get() *Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return cloneConfig(m.cfg)
}

func (m *Manager) Stop() {
	m.stopOnce.Do(func() { close(m.stop) })
}

func (m *Manager) pollForChanges(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-m.stop:
			return
		case <-ticker.C:
			stat, err := os.Stat(m.path)
			if err != nil {
				continue
			}
			m.mu.RLock()
			lastMod := m.lastModTime
			m.mu.RUnlock()
			if !stat.ModTime().After(lastMod) {
				continue
			}

			if err := m.reload(); err != nil {
				log.Error(context.Background(), "config_reload_failed", "Could not reload configuration", zap.Error(err))
				continue
			}
			m.mu.Lock()
			m.lastModTime = stat.ModTime()
			m.mu.Unlock()
		}
	}
}

func (m *Manager) reload() error {
	m.reloadMu.Lock()
	defer m.reloadMu.Unlock()
	newCfg, err := load(m.viper)
	if err != nil {
		return err
	}

	m.mu.Lock()
	oldCfg := m.cfg
	preserveRestartOnly(oldCfg, newCfg)
	m.cfg = newCfg
	m.mu.Unlock()
	logger.SetConfigLevel(newCfg.LogLevel)
	logChanges(oldCfg, newCfg, "")
	log.Info(context.Background(), "config_reloaded", "Configuration reloaded")
	return nil
}

func configuredViper(path string) (*viper.Viper, error) {
	v := viper.New()
	v.SetConfigFile(path)
	v.SetConfigType("yaml")
	v.SetEnvPrefix("FUSIONN_AIR")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()
	v.SetDefault("watcher.source", "trakt")
	v.SetDefault("log_level", "")
	v.SetDefault("trakt.base_url", "https://api.trakt.tv")
	for _, key := range []string{"scrob.base_url", "scrob.api_key", "trakt.client_id", "trakt.client_secret", "overseerr.base_url", "overseerr.api_key"} {
		if err := v.BindEnv(key); err != nil {
			return nil, fmt.Errorf("bind %s: %w", key, err)
		}
	}
	return v, nil
}

func load(v *viper.Viper) (*Config, error) {
	if err := v.ReadInConfig(); err != nil {
		return nil, err
	}
	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func preserveRestartOnly(old, next *Config) {
	next.Server = old.Server
	next.Trakt = old.Trakt
	next.Scrob = old.Scrob
	next.Overseerr = old.Overseerr
	next.Apprise = old.Apprise
	next.Scheduler.Cron = old.Scheduler.Cron
	next.Scheduler.RunOnStart = old.Scheduler.RunOnStart
	next.Watcher.Enabled = old.Watcher.Enabled
	next.Watcher.Source = old.Watcher.Source
}

func cloneConfig(cfg *Config) *Config {
	if cfg == nil {
		return nil
	}
	cloned := *cfg
	cloned.Watcher.ExcludedGenres = append([]string(nil), cfg.Watcher.ExcludedGenres...)
	cloned.Watcher.AllowedLanguages = append([]string(nil), cfg.Watcher.AllowedLanguages...)
	cloned.Watcher.Routing.AlternateGenres = append([]string(nil), cfg.Watcher.Routing.AlternateGenres...)
	cloned.Watcher.Routing.AlternateCountries = append([]string(nil), cfg.Watcher.Routing.AlternateCountries...)
	return &cloned
}

func logChanges(old, cur any, prefix string) {
	oldVal := reflect.ValueOf(old)
	newVal := reflect.ValueOf(cur)
	if oldVal.Kind() == reflect.Ptr {
		oldVal = oldVal.Elem()
	}
	if newVal.Kind() == reflect.Ptr {
		newVal = newVal.Elem()
	}
	if oldVal.Kind() != reflect.Struct {
		return
	}

	t := oldVal.Type()
	for i := range t.NumField() {
		field := t.Field(i)
		oldField := oldVal.Field(i)
		newField := newVal.Field(i)
		fieldName := field.Name
		if prefix != "" {
			fieldName = prefix + "." + fieldName
		}
		if oldField.Kind() == reflect.Struct {
			logChanges(oldField.Interface(), newField.Interface(), fieldName)
			continue
		}
		if !reflect.DeepEqual(oldField.Interface(), newField.Interface()) {
			log.Debug(context.Background(), "config_field_changed", "Configuration field changed", zap.String("field", fieldName))
		}
	}
}

// Load reads a configuration file and environment overrides.
func Load(path string) (*Config, error) {
	v, err := configuredViper(path)
	if err != nil {
		return nil, err
	}
	return load(v)
}
