package config

import (
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/spf13/viper"

	"github.com/fusionn-air/pkg/logger"
)

type Config struct {
	Server    ServerConfig    `mapstructure:"server"`
	Trakt     TraktConfig     `mapstructure:"trakt"`
	Overseerr OverseerrConfig `mapstructure:"overseerr"`
	Sonarr    SonarrConfig    `mapstructure:"sonarr"`
	Radarr    RadarrConfig    `mapstructure:"radarr"`
	Emby      EmbyConfig      `mapstructure:"emby"`
	Scheduler SchedulerConfig `mapstructure:"scheduler"`
	Watcher   WatcherConfig   `mapstructure:"watcher"`
	Cleanup   CleanupConfig   `mapstructure:"cleanup"`
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

type OverseerrConfig struct {
	BaseURL string `mapstructure:"base_url"`
	APIKey  string `mapstructure:"api_key"`
	UserID  int    `mapstructure:"user_id"`
}

type SonarrConfig struct {
	BaseURL string `mapstructure:"base_url"`
	APIKey  string `mapstructure:"api_key"`
}

type RadarrConfig struct {
	BaseURL string `mapstructure:"base_url"`
	APIKey  string `mapstructure:"api_key"`
}

type EmbyConfig struct {
	Enabled           bool     `mapstructure:"enabled"`
	BaseURL           string   `mapstructure:"base_url"`
	APIKey            string   `mapstructure:"api_key"`
	ExcludedLibraries []string `mapstructure:"excluded_libraries"`
}

type SchedulerConfig struct {
	Cron       string `mapstructure:"cron"`
	DryRun     bool   `mapstructure:"dry_run"`
	RunOnStart bool   `mapstructure:"run_on_start"`
}

type WatcherConfig struct {
	Enabled          bool          `mapstructure:"enabled"`
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

type CleanupConfig struct {
	Enabled    bool     `mapstructure:"enabled"`
	DelayDays  int      `mapstructure:"delay_days"`
	Exclusions []string `mapstructure:"exclusions"`
}

type AppriseConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	BaseURL string `mapstructure:"base_url"`
	Key     string `mapstructure:"key"`
	Tag     string `mapstructure:"tag"`
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
	v := configuredViper(path)
	cfg, err := load(v)
	if err != nil {
		return nil, err
	}

	var lastMod time.Time
	if stat, err := os.Stat(path); err == nil {
		lastMod = stat.ModTime()
	}
	m := &Manager{cfg: cfg, viper: v, stop: make(chan struct{}), path: path, lastModTime: lastMod}
	go m.pollForChanges(10 * time.Second)
	logger.Info("📋 Config loaded (polling every 10s for changes)")
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

			logger.Info("🔄 Config file changed, reloading...")
			if err := m.reload(); err != nil {
				logger.Errorf("❌ Failed to reload config: %v", err)
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
	logChanges(oldCfg, newCfg, "")
	logger.Info("✅ Config reloaded (changes take effect on next run)")
	return nil
}

func configuredViper(path string) *viper.Viper {
	v := viper.New()
	v.SetConfigFile(path)
	v.SetConfigType("yaml")
	v.SetEnvPrefix("FUSIONN_AIR")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()
	return v
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
	next.Overseerr = old.Overseerr
	next.Sonarr = old.Sonarr
	next.Radarr = old.Radarr
	next.Apprise = old.Apprise
	next.Emby.Enabled = old.Emby.Enabled
	next.Emby.BaseURL = old.Emby.BaseURL
	next.Emby.APIKey = old.Emby.APIKey
	next.Scheduler.Cron = old.Scheduler.Cron
	next.Scheduler.RunOnStart = old.Scheduler.RunOnStart
	next.Watcher.Enabled = old.Watcher.Enabled
	next.Cleanup.Enabled = old.Cleanup.Enabled
}

func cloneConfig(cfg *Config) *Config {
	if cfg == nil {
		return nil
	}
	cloned := *cfg
	cloned.Emby.ExcludedLibraries = append([]string(nil), cfg.Emby.ExcludedLibraries...)
	cloned.Watcher.ExcludedGenres = append([]string(nil), cfg.Watcher.ExcludedGenres...)
	cloned.Watcher.AllowedLanguages = append([]string(nil), cfg.Watcher.AllowedLanguages...)
	cloned.Watcher.Routing.AlternateGenres = append([]string(nil), cfg.Watcher.Routing.AlternateGenres...)
	cloned.Watcher.Routing.AlternateCountries = append([]string(nil), cfg.Watcher.Routing.AlternateCountries...)
	cloned.Cleanup.Exclusions = append([]string(nil), cfg.Cleanup.Exclusions...)
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
			logger.Infof("  📝 %s: %s → %s", fieldName, formatValue(oldField), formatValue(newField))
		}
	}
}

func formatValue(v reflect.Value) string {
	return fmt.Sprintf("%v", v.Interface())
}

func Load(path string) (*Config, error) {
	return load(configuredViper(path))
}
