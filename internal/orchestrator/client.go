package orchestrator

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log/slog"
	"os"

	"github.com/gargi-indic/gargi-flow/internal/config"
	"github.com/gargi-indic/gargi-flow/internal/logging"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/contrib/opentelemetry"
)

// NewClientOptions maps application TemporalConfig (host_port, namespace, TLS) to Temporal client.Options.
func NewClientOptions(cfg config.TemporalConfig) (client.Options, error) {
	opts := client.Options{
		HostPort:  cfg.HostPort,
		Namespace: cfg.Namespace,
	}

	if cfg.TLS != nil && cfg.TLS.Enabled {
		tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12}

		if cfg.TLS.CertFile != "" && cfg.TLS.KeyFile != "" {
			cert, err := tls.LoadX509KeyPair(cfg.TLS.CertFile, cfg.TLS.KeyFile)
			if err != nil {
				return opts, fmt.Errorf("loading TLS key pair: %w", err)
			}
			tlsCfg.Certificates = []tls.Certificate{cert}
		}

		if cfg.TLS.CAFile != "" {
			caCert, err := os.ReadFile(cfg.TLS.CAFile)
			if err != nil {
				return opts, fmt.Errorf("reading TLS CA file: %w", err)
			}
			caCertPool := x509.NewCertPool()
			if !caCertPool.AppendCertsFromPEM(caCert) {
				return opts, fmt.Errorf("appending CA cert from PEM: invalid format")
			}
			tlsCfg.RootCAs = caCertPool
		}

		opts.ConnectionOptions = client.ConnectionOptions{TLS: tlsCfg}
	}

	if cfg.TracingEnabled {
		tracingInterceptor, err := opentelemetry.NewTracingInterceptor(opentelemetry.TracerOptions{})
		if err != nil {
			return opts, fmt.Errorf("creating temporal tracing interceptor: %w", err)
		}
		opts.Interceptors = append(opts.Interceptors, tracingInterceptor)
	}

	return opts, nil
}

// NewClient establishes a connected Temporal client using client.Dial.
func NewClient(cfg config.TemporalConfig, logger *slog.Logger) (client.Client, error) {
	opts, err := NewClientOptions(cfg)
	if err != nil {
		return nil, fmt.Errorf("creating client options: %w", err)
	}

	if logger != nil {
		opts.Logger = logging.NewTemporalLoggerAdapter(logger)
	}

	c, err := client.Dial(opts)
	if err != nil {
		return nil, fmt.Errorf("dialing temporal: %w", err)
	}

	return c, nil
}
