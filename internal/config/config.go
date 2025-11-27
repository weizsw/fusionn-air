package config

import (
	"strings"

	"github.com/spf13/viper"
)

type Config struct {
	Server    ServerConfig    `mapstructure:"server"`
	Trakt     TraktConfig     `mapstructure:"trakt"`
	Overseerr OverseerrConfig `mapstructure:"overseerr"`
	Scheduler SchedulerConfig `mapstructure:"scheduler"`
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
}

type SchedulerConfig struct {
	Cron         string `mapstructure:"cron"`
	CalendarDays int    `mapstructure:"calendar_days"`
	DryRun       bool   `mapstructure:"dry_run"`
	RunOnStart   bool   `mapstructure:"run_on_start"`
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
