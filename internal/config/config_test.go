package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoad_MissingFile(t *testing.T) {
	_, err := Load("non_existent_file.yaml")
	if err == nil {
		t.Fatal("expected error when loading missing file")
	}
	if !strings.Contains(err.Error(), "failed to read config file") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestLoad_EmptyFile(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("failed to load config without path: %v", err)
	}

	if err := cfg.Validate(); err != nil {
		t.Errorf("default config should validate: %v", err)
	}

	if cfg.Store.Driver != "memory" {
		t.Errorf("expected default driver 'memory', got '%s'", cfg.Store.Driver)
	}
	if cfg.Server.HTTPAddress != ":8080" {
		t.Errorf("expected default http_address ':8080', got '%s'", cfg.Server.HTTPAddress)
	}
}

func TestLoad_PartialYAML(t *testing.T) {
	yamlContent := `
store:
  driver: redis
  redis:
    addr: localhost:6379
`
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configFile, []byte(yamlContent), 0o644); err != nil {
		t.Fatalf("failed to write test config file: %v", err)
	}

	cfg, err := Load(configFile)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	// Should have yaml values
	if cfg.Store.Driver != "redis" {
		t.Errorf("expected driver 'redis', got '%s'", cfg.Store.Driver)
	}
	// Should keep default values
	if cfg.Server.HTTPAddress != ":8080" {
		t.Errorf("expected default server address ':8080', got '%s'", cfg.Server.HTTPAddress)
	}
	if cfg.Temporal.TaskQueue != "gargi_tasks" {
		t.Errorf("expected default task queue 'gargi_tasks', got '%s'", cfg.Temporal.TaskQueue)
	}
}

func TestLoad_UnknownYAMLKey(t *testing.T) {
	yamlContent := `
store:
  driver: memory
  unknown_key: value
`
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configFile, []byte(yamlContent), 0o644); err != nil {
		t.Fatalf("failed to write test config file: %v", err)
	}

	_, err := Load(configFile)
	if err == nil {
		t.Fatal("expected error on unknown yaml key, got nil")
	}
	if !strings.Contains(err.Error(), "unknown field") && !strings.Contains(err.Error(), "not found in type") {
		t.Fatalf("expected unknown field error, got: %v", err)
	}
}

func TestLoad_YAML(t *testing.T) {
	yamlContent := `
store:
  driver: redis
  redis:
    addr: localhost:6379
temporal:
  host_port: localhost:7233
  namespace: default
  task_queue: main_queue
server:
  http_address: :8080
  grpc_address: :9090
`
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configFile, []byte(yamlContent), 0o644); err != nil {
		t.Fatalf("failed to write test config file: %v", err)
	}

	cfg, err := Load(configFile)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	if cfg.Store.Driver != "redis" {
		t.Errorf("expected driver 'redis', got '%s'", cfg.Store.Driver)
	}
	if cfg.Store.Redis == nil || cfg.Store.Redis.Addr != "localhost:6379" {
		t.Errorf("unexpected redis config: %v", cfg.Store.Redis)
	}
	if cfg.Temporal.HostPort != "localhost:7233" {
		t.Errorf("unexpected temporal host_port: %s", cfg.Temporal.HostPort)
	}
}

func TestLoad_EnvOverrides(t *testing.T) {
	yamlContent := `
store:
  driver: memory
temporal:
  host_port: localhost:7233
  namespace: default
  task_queue: main_queue
server:
  http_address: :8080
  grpc_address: :9090
`
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configFile, []byte(yamlContent), 0o644); err != nil {
		t.Fatalf("failed to write test config file: %v", err)
	}

	// Set env overrides
	t.Setenv("GARGI_STORE_DRIVER", "postgres")
	t.Setenv("GARGI_STORE_POSTGRES_DSN", "postgres://user:pass@localhost:5432/db")
	t.Setenv("GARGI_TEMPORAL_HOST_PORT", "remote:7233")
	t.Setenv("GARGI_SERVER_HTTP_ADDRESS", ":8081")

	cfg, err := Load(configFile)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	if cfg.Store.Driver != "postgres" {
		t.Errorf("expected driver 'postgres', got '%s'", cfg.Store.Driver)
	}
	if cfg.Store.Postgres == nil || cfg.Store.Postgres.DSN != "postgres://user:pass@localhost:5432/db" {
		t.Errorf("unexpected postgres config: %v", cfg.Store.Postgres)
	}
	if cfg.Temporal.HostPort != "remote:7233" {
		t.Errorf("expected remote:7233, got %s", cfg.Temporal.HostPort)
	}
	if cfg.Server.HTTPAddress != ":8081" {
		t.Errorf("expected :8081, got %s", cfg.Server.HTTPAddress)
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name        string
		cfg         Config
		expectError bool
		errContains string
	}{
		{
			name: "valid memory config",
			cfg: Config{
				Store:    StoreConfig{Driver: "memory"},
				Temporal: TemporalConfig{HostPort: "h", Namespace: "n", TaskQueue: "t"},
				Server:   ServerConfig{HTTPAddress: "h", GRPCAddress: "g"},
			},
			expectError: false,
		},
		{
			name: "valid redis config",
			cfg: Config{
				Store:    StoreConfig{Driver: "redis", Redis: &RedisConfig{Addr: "localhost:6379"}},
				Temporal: TemporalConfig{HostPort: "h", Namespace: "n", TaskQueue: "t"},
				Server:   ServerConfig{HTTPAddress: "h", GRPCAddress: "g"},
			},
			expectError: false,
		},
		{
			name: "invalid driver capitalization",
			cfg: Config{
				Store:    StoreConfig{Driver: "Redis", Redis: &RedisConfig{Addr: "localhost:6379"}},
				Temporal: TemporalConfig{HostPort: "h", Namespace: "n", TaskQueue: "t"},
				Server:   ServerConfig{HTTPAddress: "h", GRPCAddress: "g"},
			},
			expectError: true,
			errContains: "Allowed values: memory, redis, postgres, neo4j",
		},
		{
			name: "invalid driver spacing",
			cfg: Config{
				Store:    StoreConfig{Driver: " memory "},
				Temporal: TemporalConfig{HostPort: "h", Namespace: "n", TaskQueue: "t"},
				Server:   ServerConfig{HTTPAddress: "h", GRPCAddress: "g"},
			},
			expectError: true,
			errContains: "Allowed values: memory, redis, postgres, neo4j",
		},
		{
			name: "invalid driver",
			cfg: Config{
				Store:    StoreConfig{Driver: "unknown"},
				Temporal: TemporalConfig{HostPort: "h", Namespace: "n", TaskQueue: "t"},
				Server:   ServerConfig{HTTPAddress: "h", GRPCAddress: "g"},
			},
			expectError: true,
			errContains: "Allowed values: memory, redis, postgres, neo4j",
		},
		{
			name: "missing redis addr",
			cfg: Config{
				Store:    StoreConfig{Driver: "redis"},
				Temporal: TemporalConfig{HostPort: "h", Namespace: "n", TaskQueue: "t"},
				Server:   ServerConfig{HTTPAddress: "h", GRPCAddress: "g"},
			},
			expectError: true,
			errContains: "redis configuration requires addr",
		},
		{
			name: "missing postgres dsn",
			cfg: Config{
				Store:    StoreConfig{Driver: "postgres"},
				Temporal: TemporalConfig{HostPort: "h", Namespace: "n", TaskQueue: "t"},
				Server:   ServerConfig{HTTPAddress: "h", GRPCAddress: "g"},
			},
			expectError: true,
			errContains: "postgres configuration requires dsn",
		},
		{
			name: "missing neo4j settings",
			cfg: Config{
				Store:    StoreConfig{Driver: "neo4j", Neo4j: &Neo4jConfig{URI: "uri"}}, // missing user/pass
				Temporal: TemporalConfig{HostPort: "h", Namespace: "n", TaskQueue: "t"},
				Server:   ServerConfig{HTTPAddress: "h", GRPCAddress: "g"},
			},
			expectError: true,
			errContains: "neo4j configuration requires uri, user, and password",
		},
		{
			name: "missing temporal host_port",
			cfg: Config{
				Store:    StoreConfig{Driver: "memory"},
				Temporal: TemporalConfig{Namespace: "n", TaskQueue: "t"},
				Server:   ServerConfig{HTTPAddress: "h", GRPCAddress: "g"},
			},
			expectError: true,
			errContains: "temporal host_port is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.expectError {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("expected error to contain '%s', got '%v'", tt.errContains, err)
				}
			} else {
				if err != nil {
					t.Fatalf("expected no error, got %v", err)
				}
			}
		})
	}
}

func TestTemporalConfigEffectiveQueues(t *testing.T) {
	tc := TemporalConfig{TaskQueue: "fallback"}

	if tc.EffectiveWorkflowTaskQueue() != "fallback" {
		t.Errorf("expected fallback")
	}
	if tc.EffectiveActivityTaskQueue() != "fallback" {
		t.Errorf("expected fallback")
	}

	tc.WorkflowTaskQueue = "wf-queue"
	if tc.EffectiveWorkflowTaskQueue() != "wf-queue" {
		t.Errorf("expected wf-queue")
	}
	if tc.EffectiveActivityTaskQueue() != "fallback" {
		t.Errorf("expected fallback")
	}

	tc.ActivityTaskQueue = "act-queue"
	if tc.EffectiveActivityTaskQueue() != "act-queue" {
		t.Errorf("expected act-queue")
	}
}

func TestApplyEnvOverridesTLS(t *testing.T) {
	t.Setenv("GARGI_TEMPORAL_TLS_ENABLED", "true")
	t.Setenv("GARGI_TEMPORAL_TLS_CERT_FILE", "cert.pem")
	t.Setenv("GARGI_TEMPORAL_TLS_KEY_FILE", "key.pem")
	t.Setenv("GARGI_TEMPORAL_TLS_CA_FILE", "ca.pem")

	cfg := Default()
	applyEnvOverrides(cfg)

	if !cfg.Temporal.TLS.Enabled {
		t.Errorf("expected TLS enabled")
	}
	if cfg.Temporal.TLS.CertFile != "cert.pem" {
		t.Errorf("expected cert.pem")
	}
	if cfg.Temporal.TLS.KeyFile != "key.pem" {
		t.Errorf("expected key.pem")
	}
	if cfg.Temporal.TLS.CAFile != "ca.pem" {
		t.Errorf("expected ca.pem")
	}
}

func TestValidateTLS(t *testing.T) {
	cfg := Default()
	cfg.Temporal.TLS = &TLSConfig{Enabled: true, CertFile: "cert.pem"}
	err := cfg.Validate()
	if err == nil || err.Error() != "temporal tls requires both cert_file and key_file when client certificate is configured" {
		t.Errorf("expected error, got: %v", err)
	}

	cfg.Temporal.TLS.KeyFile = "key.pem"
	err = cfg.Validate()
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
}

func TestTemporalConfigBuildID(t *testing.T) {
	t.Setenv("GARGI_TEMPORAL_BUILD_ID", "v1.2.3")
	t.Setenv("GARGI_TEMPORAL_USE_BUILD_ID_FOR_VERSIONING", "true")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Temporal.BuildID != "v1.2.3" {
		t.Errorf("expected build_id 'v1.2.3', got '%s'", cfg.Temporal.BuildID)
	}
	if !cfg.Temporal.UseBuildIDForVersioning {
		t.Errorf("expected use_build_id_for_versioning to be true")
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("expected config with build_id to validate: %v", err)
	}
}

func TestValidateBuildID(t *testing.T) {
	cfg := Default()
	cfg.Temporal.UseBuildIDForVersioning = true
	cfg.Temporal.BuildID = ""

	err := cfg.Validate()
	if err == nil || err.Error() != "temporal build_id is required when use_build_id_for_versioning is enabled" {
		t.Errorf("expected missing build_id error, got: %v", err)
	}
}

func TestLoad_CapabilitiesYAML(t *testing.T) {
	yamlContent := `
store:
  capabilities:
    state: redis
    lock: redis
    events: redis
    execution_log: postgres
  redis:
    addr: localhost:6379
  postgres:
    dsn: postgres://user:pass@localhost:5432/db
temporal:
  host_port: localhost:7233
  namespace: default
  task_queue: main_queue
server:
  http_address: :8080
  grpc_address: :9090
`
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configFile, []byte(yamlContent), 0o644); err != nil {
		t.Fatalf("failed to write test config file: %v", err)
	}

	cfg, err := Load(configFile)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	if cfg.Store.EffectiveStateDriver() != "redis" {
		t.Errorf("expected effective state driver 'redis', got '%s'", cfg.Store.EffectiveStateDriver())
	}
	if cfg.Store.EffectiveLockDriver() != "redis" {
		t.Errorf("expected effective lock driver 'redis', got '%s'", cfg.Store.EffectiveLockDriver())
	}
	if cfg.Store.EffectiveEventsDriver() != "redis" {
		t.Errorf("expected effective events driver 'redis', got '%s'", cfg.Store.EffectiveEventsDriver())
	}
	if cfg.Store.EffectiveExecutionLogDriver() != "postgres" {
		t.Errorf("expected effective log driver 'postgres', got '%s'", cfg.Store.EffectiveExecutionLogDriver())
	}

	drivers := cfg.Store.DriversInUse()
	if len(drivers) != 2 || drivers[0] != "redis" || drivers[1] != "postgres" {
		t.Errorf("unexpected drivers in use: %v", drivers)
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected valid config, got error: %v", err)
	}
}

func TestLoad_CapabilitiesEnvOverrides(t *testing.T) {
	t.Setenv("GARGI_STORE_CAPABILITIES_STATE", "redis")
	t.Setenv("GARGI_STORE_CAPABILITIES_EXECUTION_LOG", "postgres")
	t.Setenv("GARGI_STORE_REDIS_ADDR", "redis.local:6379")
	t.Setenv("GARGI_STORE_POSTGRES_DSN", "postgres://localhost:5432/test")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("failed to load default config: %v", err)
	}

	if cfg.Store.Capabilities.State != "redis" {
		t.Errorf("expected state driver 'redis', got '%s'", cfg.Store.Capabilities.State)
	}
	if cfg.Store.Capabilities.ExecutionLog != "postgres" {
		t.Errorf("expected log driver 'postgres', got '%s'", cfg.Store.Capabilities.ExecutionLog)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected config to validate, got: %v", err)
	}
}

func TestValidate_CapabilitiesFailFast(t *testing.T) {
	baseCfg := func() Config {
		return Config{
			Temporal: TemporalConfig{HostPort: "h", Namespace: "n", TaskQueue: "t"},
			Server:   ServerConfig{HTTPAddress: "h", GRPCAddress: "g"},
		}
	}

	t.Run("neo4j for lock capability", func(t *testing.T) {
		cfg := baseCfg()
		cfg.Store.Capabilities.Lock = "neo4j"
		err := cfg.Validate()
		if err == nil || !strings.Contains(err.Error(), "neo4j does not support capability 'lock'") {
			t.Fatalf("expected neo4j lock capability error, got: %v", err)
		}
	})

	t.Run("neo4j for events capability", func(t *testing.T) {
		cfg := baseCfg()
		cfg.Store.Capabilities.Events = "neo4j"
		err := cfg.Validate()
		if err == nil || !strings.Contains(err.Error(), "neo4j does not support capability 'events'") {
			t.Fatalf("expected neo4j events capability error, got: %v", err)
		}
	})

	t.Run("invalid capability driver", func(t *testing.T) {
		cfg := baseCfg()
		cfg.Store.Capabilities.State = "cassandra"
		err := cfg.Validate()
		if err == nil || !strings.Contains(err.Error(), "invalid store state capability driver: cassandra") {
			t.Fatalf("expected invalid capability driver error, got: %v", err)
		}
	})

	t.Run("composite missing redis addr", func(t *testing.T) {
		cfg := baseCfg()
		cfg.Store.Capabilities.State = "redis"
		cfg.Store.Capabilities.ExecutionLog = "postgres"
		cfg.Store.Postgres = &PostgresConfig{DSN: "postgres://..."}
		err := cfg.Validate()
		if err == nil || !strings.Contains(err.Error(), "redis configuration requires addr") {
			t.Fatalf("expected redis addr error, got: %v", err)
		}
	})

	t.Run("composite missing postgres dsn", func(t *testing.T) {
		cfg := baseCfg()
		cfg.Store.Capabilities.State = "redis"
		cfg.Store.Capabilities.ExecutionLog = "postgres"
		cfg.Store.Redis = &RedisConfig{Addr: "localhost:6379"}
		err := cfg.Validate()
		if err == nil || !strings.Contains(err.Error(), "postgres configuration requires dsn") {
			t.Fatalf("expected postgres dsn error, got: %v", err)
		}
	})
}

