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
	Enabled      bool          `mapstructure:"enabled"`
	CalendarDays int           `mapstructure:"calendar_days"` // Days ahead to check for new episodes
	Routing      RoutingConfig `mapstructure:"routing"`
}

type RoutingConfig struct {
	DefaultServerID    int      `mapstructure:"default_server_id"`
	AlternateServerID  int      `mapstructure:"alternate_server_id"`
	AlternateGenres    []string `mapstructure:"alternate_genres"`
	AlternateCountries []string `mapstructure:"alternate_countries"`
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

// Manager handles config loading and hot-reload via polling.
// Services should call Get() at execution time to get fresh config values.
//
// Hot-reloadable settings (no restart needed):
//   - scheduler.dry_run, watcher.calendar_days
//   - cleanup.delay_days, cleanup.exclusions
//
// Requires restart:
//   - server.port, scheduler.cron
//   - All API credentials (trakt, overseerr, sonarr, radarr, apprise)
//   - All *.enabled toggles
type Manager struct {
	mu   sync.RWMutex
	cfg  *Config
	stop chan struct{}

	// Polling state
	path        string
	lastModTime time.Time
}

// NewManager creates a config manager with hot-reload support via polling.
// Config changes are detected automatically every 10 seconds.
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

	// Get initial file mod time
	var lastMod time.Time
	if stat, err := os.Stat(path); err == nil {
		lastMod = stat.ModTime()
	}

	m := &Manager{
		cfg:         &cfg,
		stop:        make(chan struct{}),
		path:        path,
		lastModTime: lastMod,
	}

	// Start polling for config changes
	go m.pollForChanges(10 * time.Second)

	logger.Infof("📋 Config loaded (polling every 10s for changes)")

	return m, nil
}

// Get returns the current config (thread-safe).
// Call this at execution time to get fresh config values.
func (m *Manager) Get() *Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cfg
}

// Stop stops the config polling goroutine.
func (m *Manager) Stop() {
	close(m.stop)
}

// pollForChanges checks file modtime periodically for Docker bind mount compatibility.
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

			if stat.ModTime().After(lastMod) {
				logger.Infof("🔄 Config file changed, reloading...")

				if err := viper.ReadInConfig(); err != nil {
					logger.Errorf("❌ Failed to re-read config: %v", err)
					continue
				}

				m.mu.Lock()
				m.lastModTime = stat.ModTime()
				m.mu.Unlock()

				m.reload()
			}
		}
	}
}

// reload re-reads config and logs what changed.
func (m *Manager) reload() {
	var newCfg Config
	if err := viper.Unmarshal(&newCfg); err != nil {
		logger.Errorf("❌ Failed to reload config: %v", err)
		return
	}

	m.mu.Lock()
	oldCfg := m.cfg
	m.cfg = &newCfg
	m.mu.Unlock()

	// Log what changed
	logChanges(oldCfg, &newCfg, "")
	logger.Info("✅ Config reloaded (changes take effect on next run)")
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

// formatValue formats a reflect.Value for logging.
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
