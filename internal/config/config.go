package config

import (
	"fmt"
	"reflect"
	"strings"
	"sync"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/viper"

	"github.com/fusionn-air/pkg/logger"
)

type Config struct {
	Server    ServerConfig    `mapstructure:"server"`
	Trakt     TraktConfig     `mapstructure:"trakt"`
	Overseerr OverseerrConfig `mapstructure:"overseerr"`
	Sonarr    SonarrConfig    `mapstructure:"sonarr"`
	Radarr    RadarrConfig    `mapstructure:"radarr"`
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
	UserID  int    `mapstructure:"user_id"` // Request as specific user (0 = API key owner)
}

type SonarrConfig struct {
	BaseURL string `mapstructure:"base_url"`
	APIKey  string `mapstructure:"api_key"`
}

type RadarrConfig struct {
	BaseURL string `mapstructure:"base_url"`
	APIKey  string `mapstructure:"api_key"`
}

type SchedulerConfig struct {
	Cron       string `mapstructure:"cron"`
	DryRun     bool   `mapstructure:"dry_run"`
	RunOnStart bool   `mapstructure:"run_on_start"`
}

type WatcherConfig struct {
	Enabled      bool `mapstructure:"enabled"`
	CalendarDays int  `mapstructure:"calendar_days"` // Days ahead to check for new episodes
}

type CleanupConfig struct {
	Enabled    bool     `mapstructure:"enabled"`
	DelayDays  int      `mapstructure:"delay_days"` // Days to wait after fully watched
	Exclusions []string `mapstructure:"exclusions"` // Series titles to never remove
}

type AppriseConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	BaseURL string `mapstructure:"base_url"` // Apprise API URL (e.g., http://apprise:8000)
	Key     string `mapstructure:"key"`      // Apprise config key (default: apprise)
	Tag     string `mapstructure:"tag"`      // Tag to filter services (default: all)
}

// ChangeCallback is called when config changes. Receives old and new config.
type ChangeCallback func(old, new *Config)

// Manager handles config loading and hot-reload.
type Manager struct {
	mu        sync.RWMutex
	cfg       *Config
	callbacks []ChangeCallback
}

// NewManager creates a config manager with hot-reload support.
func NewManager(path string) (*Manager, error) {
	viper.SetConfigFile(path)
	viper.SetConfigType("yaml")

	// Environment variable override support
	viper.SetEnvPrefix("FUSIONN_AIR")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err != nil {
		return nil, err
	}

	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, err
	}

	m := &Manager{cfg: &cfg}

	// Setup hot-reload
	viper.OnConfigChange(func(e fsnotify.Event) {
		logger.Infof("🔄 Config file changed: %s", e.Name)
		m.reload()
	})
	viper.WatchConfig()

	return m, nil
}

// Get returns the current config (thread-safe).
func (m *Manager) Get() *Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cfg
}

// OnChange registers a callback for config changes.
func (m *Manager) OnChange(cb ChangeCallback) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.callbacks = append(m.callbacks, cb)
}

// reload re-reads config and notifies subscribers.
func (m *Manager) reload() {
	var newCfg Config
	if err := viper.Unmarshal(&newCfg); err != nil {
		logger.Errorf("❌ Failed to reload config: %v", err)
		return
	}

	m.mu.Lock()
	oldCfg := m.cfg
	m.cfg = &newCfg
	callbacks := m.callbacks
	m.mu.Unlock()

	// Log what changed
	logChanges(oldCfg, &newCfg, "")

	// Notify subscribers outside lock
	for _, cb := range callbacks {
		cb(oldCfg, &newCfg)
	}
}

// logChanges logs field-level differences between old and new config.
func logChanges(old, cur any, prefix string) {
	oldVal := reflect.ValueOf(old)
	newVal := reflect.ValueOf(cur)

	// Dereference pointers
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

		// Recurse into nested structs
		if oldField.Kind() == reflect.Struct {
			logChanges(oldField.Interface(), newField.Interface(), fieldName)
			continue
		}

		// Compare values
		if !reflect.DeepEqual(oldField.Interface(), newField.Interface()) {
			oldStr := formatValue(oldField)
			newStr := formatValue(newField)
			logger.Infof("  📝 %s: %s → %s", fieldName, oldStr, newStr)
		}
	}
}

// formatValue formats a reflect.Value for logging, masking sensitive fields.
func formatValue(v reflect.Value) string {
	if v.Kind() == reflect.Slice {
		return fmt.Sprintf("%v", v.Interface())
	}
	return fmt.Sprintf("%v", v.Interface())
}

// Load is a convenience function for one-time loading (backwards compatible).
func Load(path string) (*Config, error) {
	viper.SetConfigFile(path)
	viper.SetConfigType("yaml")

	viper.SetEnvPrefix("FUSIONN_AIR")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err != nil {
		return nil, err
	}

	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}
