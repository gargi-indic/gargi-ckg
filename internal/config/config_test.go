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
