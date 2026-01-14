package config

import (
	"github.com/spf13/viper"
)

// Config holds all the configuration for our application
// The structure tags (mapstructure) tell Viper which YAML field maps to which Go struct field.
type Config struct {
	Server    ServerConfig    `mapstructure:"server"`
	Proxy     ProxyConfig     `mapstructure:"proxy"`
	RateLimit RateLimitConfig `mapstructure:"ratelimit"`
	Redis RedisConfig `mapstructure:"redis"`
}

type ServerConfig struct {
	Port string `mapstructure:"port"`
}

type ProxyConfig struct {
	Target string `mapstructure:"target"`
}

type RateLimitConfig struct {
	Enabled bool    `mapstructure:"enabled"`
	RPS     float64 `mapstructure:"requests_per_second"`
	Burst   int     `mapstructure:"burst"`
}
type RedisConfig struct {
    Address  string `mapstructure:"address"`
    Password string `mapstructure:"password"`
    DB       int    `mapstructure:"db"`
    Enabled  bool   `mapstructure:"enabled"`
}
// Load reads the config.yaml file and unmarshals it into the Config struct
func Load() (*Config, error) {
	viper.AddConfigPath("./configs") // Look in the configs folder
	viper.SetConfigName("config")    // Look for a file named "config"
	viper.SetConfigType("yaml")      // It's a YAML file

	// 1. Read the file
	if err := viper.ReadInConfig(); err != nil {
		return nil, err
	}

	// 2. Parse it into our struct
	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}