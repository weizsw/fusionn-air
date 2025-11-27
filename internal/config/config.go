package config

import (
	"strings"

	"github.com/spf13/viper"
)

type Config struct {
	Server    ServerConfig    `mapstructure:"server"`
	Trakt     TraktConfig     `mapstructure:"trakt"`
	Overseerr OverseerrConfig `mapstructure:"overseerr"`
	Sonarr    SonarrConfig    `mapstructure:"sonarr"`
	Scheduler SchedulerConfig `mapstructure:"scheduler"`
	Watcher   WatcherConfig   `mapstructure:"watcher"`
	Cleanup   CleanupConfig   `mapstructure:"cleanup"`
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

func Load(path string) (*Config, error) {
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

	return &cfg, nil
}
