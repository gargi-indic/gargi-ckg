package orchestrator

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gargi-indic/gargi-ckg/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/workflow"
)

func TestNewClientOptions(t *testing.T) {
	cfg := config.TemporalConfig{
		HostPort:  "localhost:7233",
		Namespace: "test-namespace",
	}

	opts, err := NewClientOptions(cfg)
	require.NoError(t, err)
	assert.Equal(t, "localhost:7233", opts.HostPort)
	assert.Equal(t, "test-namespace", opts.Namespace)
	assert.Nil(t, opts.ConnectionOptions.TLS)

	// Generate test certs
	tempDir := t.TempDir()
	certPath, keyPath, caPath := generateTestCerts(t, tempDir)

	// TLS Enabled
	cfg.TLS = &config.TLSConfig{
		Enabled:  true,
		CertFile: certPath,
		KeyFile:  keyPath,
		CAFile:   caPath,
	}

	opts, err = NewClientOptions(cfg)
	require.NoError(t, err)
	assert.NotNil(t, opts.ConnectionOptions.TLS)
	assert.Len(t, opts.ConnectionOptions.TLS.Certificates, 1)
	assert.NotNil(t, opts.ConnectionOptions.TLS.RootCAs)

	// Invalid cert path
	cfg.TLS.CertFile = "nonexistent.pem"
	_, err = NewClientOptions(cfg)
	assert.Error(t, err)
}

func generateTestCerts(t *testing.T, dir string) (string, string, string) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"Acme Co"},
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	require.NoError(t, err)

	certPath := filepath.Join(dir, "cert.pem")
	certOut, err := os.Create(certPath)
	require.NoError(t, err)
	require.NoError(t, pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes}))
	require.NoError(t, certOut.Close())

	keyPath := filepath.Join(dir, "key.pem")
	keyOut, err := os.OpenFile(keyPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	require.NoError(t, err)
	privBytes, err := x509.MarshalPKCS8PrivateKey(priv)
	require.NoError(t, err)
	require.NoError(t, pem.Encode(keyOut, &pem.Block{Type: "PRIVATE KEY", Bytes: privBytes}))
	require.NoError(t, keyOut.Close())

	caPath := filepath.Join(dir, "ca.pem")
	caOut, err := os.Create(caPath)
	require.NoError(t, err)
	require.NoError(t, pem.Encode(caOut, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes}))
	require.NoError(t, caOut.Close())

	return certPath, keyPath, caPath
}

func testWorkflow(_ workflow.Context) error {
	return nil
}

func testActivity(_ context.Context) error {
	return nil
}

func TestNewWorkerManager(t *testing.T) {
	c, err := client.NewLazyClient(client.Options{})
	require.NoError(t, err)
	defer c.Close()

	// Shared queues
	cfg := config.TemporalConfig{TaskQueue: "shared-queue"}
	wm := NewWorkerManager(c, cfg, nil)
	assert.Equal(t, "shared-queue", wm.WorkflowTaskQueue())
	assert.Equal(t, "shared-queue", wm.ActivityTaskQueue())
	assert.True(t, wm.sharedWorker)

	// Separate queues
	cfg = config.TemporalConfig{WorkflowTaskQueue: "wf-queue", ActivityTaskQueue: "act-queue"}
	wm = NewWorkerManager(c, cfg, nil)
	assert.Equal(t, "wf-queue", wm.WorkflowTaskQueue())
	assert.Equal(t, "act-queue", wm.ActivityTaskQueue())
	assert.False(t, wm.sharedWorker)

	wm.RegisterWorkflow(testWorkflow)
	wm.RegisterActivity(testActivity)

	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		errCh <- wm.Run(ctx)
	}()

	time.Sleep(10 * time.Millisecond)
	cancel()

	<-errCh
}

func TestNewWorkerManager_BuildID(t *testing.T) {
	c, err := client.NewLazyClient(client.Options{})
	require.NoError(t, err)
	defer c.Close()

	cfg := config.TemporalConfig{
		TaskQueue:               "versioned-queue",
		BuildID:                 "build-1.0.0",
		UseBuildIDForVersioning: true,
	}
	wm := NewWorkerManager(c, cfg, nil)
	assert.Equal(t, "versioned-queue", wm.WorkflowTaskQueue())
	assert.True(t, wm.sharedWorker)
}
