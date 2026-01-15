package config

import (
	"log"
	"sync"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/viper"
)

// Config holds all the configuration for our application
// The structure tags (mapstructure) tell Viper which YAML field maps to which Go struct field.
type Config struct {
	Server    ServerConfig       `mapstructure:"server"`
	Proxy     ProxyConfig        `mapstructure:"proxy"`
	RateLimit RateLimitConfig    `mapstructure:"ratelimit"`
	Redis     RedisConfig        `mapstructure:"redis"`
	Models    map[string]float64 `mapstructure:"models"`
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

// Store wraps configuration with thread-safe access and hot-reload updates.
type Store struct {
	mu  sync.RWMutex
	cfg *Config
}

func (s *Store) Get() *Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.cfg == nil {
		return nil
	}
	cpy := *s.cfg
	return &cpy
}

func (s *Store) set(cfg *Config) {
	s.mu.Lock()
	s.cfg = cfg
	s.mu.Unlock()
}

// LoadAndWatch loads the config and watches for on-disk changes.
func LoadAndWatch() (*Store, error) {
	v := viper.NewWithOptions(viper.KeyDelimiter("::"))
	v.AddConfigPath("./configs")
	v.SetConfigName("config")
	v.SetConfigType("yaml")

	if err := v.ReadInConfig(); err != nil {
		return nil, err
	}

	store := &Store{}
	if err := refresh(v, store); err != nil {
		return nil, err
	}

	v.WatchConfig()
	v.OnConfigChange(func(e fsnotify.Event) {
		if err := refresh(v, store); err != nil {
			log.Printf("[CONFIG] reload failed: %v", err)
		} else {
			log.Printf("[CONFIG] reloaded from %s", e.Name)
		}
	})

	return store, nil
}

// Load preserves the old API: it loads once and does not watch.
func Load() (*Config, error) {
	store, err := LoadAndWatch()
	if err != nil {
		return nil, err
	}
	return store.Get(), nil
}


func refresh(v *viper.Viper, store *Store) error {
	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return err
	}
	store.set(&cfg)
	return nil
}
