// Package config provides configuration definitions and loading functions.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"

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

// TLSConfig holds optional TLS settings for the Temporal client connection.
type TLSConfig struct {
	Enabled  bool   `yaml:"enabled"`
	CertFile string `yaml:"cert_file,omitempty"`
	KeyFile  string `yaml:"key_file,omitempty"`
	CAFile   string `yaml:"ca_file,omitempty"`
}

// TemporalConfig holds the configuration for the Temporal connection.
type TemporalConfig struct {
	HostPort          string     `yaml:"host_port"`
	Namespace         string     `yaml:"namespace"`
	TaskQueue         string     `yaml:"task_queue"`
	WorkflowTaskQueue       string     `yaml:"workflow_task_queue,omitempty"`
	ActivityTaskQueue       string     `yaml:"activity_task_queue,omitempty"`
	TLS                     *TLSConfig `yaml:"tls,omitempty"`
	BuildID                 string     `yaml:"build_id,omitempty"`
	UseBuildIDForVersioning bool       `yaml:"use_build_id_for_versioning,omitempty"`
}

// EffectiveWorkflowTaskQueue returns WorkflowTaskQueue if non-empty, otherwise TaskQueue.
func (t TemporalConfig) EffectiveWorkflowTaskQueue() string {
	if t.WorkflowTaskQueue != "" {
		return t.WorkflowTaskQueue
	}
	return t.TaskQueue
}

// EffectiveActivityTaskQueue returns ActivityTaskQueue if non-empty, otherwise TaskQueue.
func (t TemporalConfig) EffectiveActivityTaskQueue() string {
	if t.ActivityTaskQueue != "" {
		return t.ActivityTaskQueue
	}
	return t.TaskQueue
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

// Default returns a new configuration instance with default values populated.
func Default() *Config {
	return &Config{
		Store: StoreConfig{
			Driver: "memory",
		},
		Temporal: TemporalConfig{
			HostPort:  "localhost:7233",
			Namespace: "default",
			TaskQueue: "gargi_tasks",
		},
		Server: ServerConfig{
			HTTPAddress: ":8080",
			GRPCAddress: ":9090",
		},
	}
}

// Load reads the configuration from a YAML file (if path is provided)
// and applies environment variable overrides on top of defaults.
func Load(path string) (*Config, error) {
	cfg := Default()

	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("failed to read config file: %w", err)
		}

		dec := yaml.NewDecoder(bytes.NewReader(data))
		dec.KnownFields(true)

		if err := dec.Decode(cfg); err != nil && !errors.Is(err, io.EOF) {
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
	if val, ok := os.LookupEnv("GARGI_TEMPORAL_WORKFLOW_TASK_QUEUE"); ok {
		cfg.Temporal.WorkflowTaskQueue = val
	}
	if val, ok := os.LookupEnv("GARGI_TEMPORAL_ACTIVITY_TASK_QUEUE"); ok {
		cfg.Temporal.ActivityTaskQueue = val
	}
	if val, ok := os.LookupEnv("GARGI_TEMPORAL_TLS_ENABLED"); ok {
		if cfg.Temporal.TLS == nil {
			cfg.Temporal.TLS = &TLSConfig{}
		}
		cfg.Temporal.TLS.Enabled = val == "true" || val == "1"
	}
	if val, ok := os.LookupEnv("GARGI_TEMPORAL_TLS_CERT_FILE"); ok {
		if cfg.Temporal.TLS == nil {
			cfg.Temporal.TLS = &TLSConfig{}
		}
		cfg.Temporal.TLS.CertFile = val
	}
	if val, ok := os.LookupEnv("GARGI_TEMPORAL_TLS_KEY_FILE"); ok {
		if cfg.Temporal.TLS == nil {
			cfg.Temporal.TLS = &TLSConfig{}
		}
		cfg.Temporal.TLS.KeyFile = val
	}
	if val, ok := os.LookupEnv("GARGI_TEMPORAL_TLS_CA_FILE"); ok {
		if cfg.Temporal.TLS == nil {
			cfg.Temporal.TLS = &TLSConfig{}
		}
		cfg.Temporal.TLS.CAFile = val
	}
	if val, ok := os.LookupEnv("GARGI_TEMPORAL_BUILD_ID"); ok {
		cfg.Temporal.BuildID = val
	}
	if val, ok := os.LookupEnv("GARGI_TEMPORAL_USE_BUILD_ID_FOR_VERSIONING"); ok {
		cfg.Temporal.UseBuildIDForVersioning = val == "true" || val == "1"
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
	driver := c.Store.Driver

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
	if c.Temporal.EffectiveWorkflowTaskQueue() == "" || c.Temporal.EffectiveActivityTaskQueue() == "" {
		return errors.New("temporal task_queue (or both workflow_task_queue and activity_task_queue) is required")
	}

	if c.Temporal.TLS != nil && c.Temporal.TLS.Enabled {
		if (c.Temporal.TLS.CertFile == "") != (c.Temporal.TLS.KeyFile == "") {
			return errors.New("temporal tls requires both cert_file and key_file when client certificate is configured")
		}
	}

	if c.Temporal.UseBuildIDForVersioning && c.Temporal.BuildID == "" {
		return errors.New("temporal build_id is required when use_build_id_for_versioning is enabled")
	}

	if c.Server.HTTPAddress == "" {
		return errors.New("server http_address is required")
	}
	if c.Server.GRPCAddress == "" {
		return errors.New("server grpc_address is required")
	}

	return nil
}
