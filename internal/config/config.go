// Package config provides configuration definitions and loading functions.
package config

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// StoreConfig holds the configuration for the storage adapter.
type StoreConfig struct {
	Driver   string          `yaml:"driver"`
	Redis    *RedisConfig    `yaml:"redis,omitempty"`
	Postgres *PostgresConfig `yaml:"postgres,omitempty"`
	Neo4j    *Neo4jConfig    `yaml:"neo4j,omitempty"`
}

// RedisConfig holds connection settings for Redis.
type RedisConfig struct {
	Addr string `yaml:"addr"`
}

// PostgresConfig holds connection settings for Postgres.
type PostgresConfig struct {
	DSN string `yaml:"dsn"`
}

// Neo4jConfig holds connection settings for Neo4j.
type Neo4jConfig struct {
	URI      string `yaml:"uri"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
}

// TemporalConfig holds the configuration for the Temporal connection.
type TemporalConfig struct {
	HostPort  string `yaml:"host_port"`
	Namespace string `yaml:"namespace"`
	TaskQueue string `yaml:"task_queue"`
}

// ServerConfig holds the configuration for the API server.
type ServerConfig struct {
	HTTPAddress string `yaml:"http_address"`
	GRPCAddress string `yaml:"grpc_address"`
}

// Config represents the root configuration structure.
type Config struct {
	Store    StoreConfig    `yaml:"store"`
	Temporal TemporalConfig `yaml:"temporal"`
	Server   ServerConfig   `yaml:"server"`
}

// Load reads the configuration from a YAML file (if path is provided)
// and applies environment variable overrides.
func Load(path string) (*Config, error) {
	cfg := &Config{}

	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("failed to read config file: %w", err)
		}
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("failed to parse config file: %w", err)
		}
	}

	applyEnvOverrides(cfg)

	return cfg, nil
}

// applyEnvOverrides updates configuration based on environment variables.
func applyEnvOverrides(cfg *Config) {
	if val, ok := os.LookupEnv("GARGI_STORE_DRIVER"); ok {
		cfg.Store.Driver = val
	}
	if val, ok := os.LookupEnv("GARGI_STORE_REDIS_ADDR"); ok {
		if cfg.Store.Redis == nil {
			cfg.Store.Redis = &RedisConfig{}
		}
		cfg.Store.Redis.Addr = val
	}
	if val, ok := os.LookupEnv("GARGI_STORE_POSTGRES_DSN"); ok {
		if cfg.Store.Postgres == nil {
			cfg.Store.Postgres = &PostgresConfig{}
		}
		cfg.Store.Postgres.DSN = val
	}
	if val, ok := os.LookupEnv("GARGI_STORE_NEO4J_URI"); ok {
		if cfg.Store.Neo4j == nil {
			cfg.Store.Neo4j = &Neo4jConfig{}
		}
		cfg.Store.Neo4j.URI = val
	}
	if val, ok := os.LookupEnv("GARGI_STORE_NEO4J_USER"); ok {
		if cfg.Store.Neo4j == nil {
			cfg.Store.Neo4j = &Neo4jConfig{}
		}
		cfg.Store.Neo4j.User = val
	}
	if val, ok := os.LookupEnv("GARGI_STORE_NEO4J_PASSWORD"); ok {
		if cfg.Store.Neo4j == nil {
			cfg.Store.Neo4j = &Neo4jConfig{}
		}
		cfg.Store.Neo4j.Password = val
	}

	if val, ok := os.LookupEnv("GARGI_TEMPORAL_HOST_PORT"); ok {
		cfg.Temporal.HostPort = val
	}
	if val, ok := os.LookupEnv("GARGI_TEMPORAL_NAMESPACE"); ok {
		cfg.Temporal.Namespace = val
	}
	if val, ok := os.LookupEnv("GARGI_TEMPORAL_TASK_QUEUE"); ok {
		cfg.Temporal.TaskQueue = val
	}

	if val, ok := os.LookupEnv("GARGI_SERVER_HTTP_ADDRESS"); ok {
		cfg.Server.HTTPAddress = val
	}
	if val, ok := os.LookupEnv("GARGI_SERVER_GRPC_ADDRESS"); ok {
		cfg.Server.GRPCAddress = val
	}
}

// Validate checks the configuration for required fields and valid options.
func (c *Config) Validate() error {
	driver := strings.ToLower(strings.TrimSpace(c.Store.Driver))

	switch driver {
	case "memory":
		// valid
	case "redis":
		if c.Store.Redis == nil || c.Store.Redis.Addr == "" {
			return errors.New("redis configuration requires addr")
		}
	case "postgres":
		if c.Store.Postgres == nil || c.Store.Postgres.DSN == "" {
			return errors.New("postgres configuration requires dsn")
		}
	case "neo4j":
		if c.Store.Neo4j == nil || c.Store.Neo4j.URI == "" || c.Store.Neo4j.User == "" || c.Store.Neo4j.Password == "" {
			return errors.New("neo4j configuration requires uri, user, and password")
		}
	default:
		return fmt.Errorf("invalid store driver: %s. Allowed values: memory, redis, postgres, neo4j", c.Store.Driver)
	}

	if c.Temporal.HostPort == "" {
		return errors.New("temporal host_port is required")
	}
	if c.Temporal.Namespace == "" {
		return errors.New("temporal namespace is required")
	}
	if c.Temporal.TaskQueue == "" {
		return errors.New("temporal task_queue is required")
	}

	if c.Server.HTTPAddress == "" {
		return errors.New("server http_address is required")
	}
	if c.Server.GRPCAddress == "" {
		return errors.New("server grpc_address is required")
	}

	return nil
}
