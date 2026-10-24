//go:build integration

package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres"
	"github.com/gargi-indic/gargi-flow/internal/sim/jobs"
	"github.com/gargi-indic/gargi-flow/internal/store/postgres"
	"github.com/gargi-indic/gargi-flow/pkg/sim"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

// loanDir is the loan underwriting example, the acceptance model.
var loanDir = filepath.Join("..", "..", "examples", "simulation", "loan")

func readLoan(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(loanDir, name))
	require.NoError(t, err)
	return string(data)
}

// freeAddr returns a currently unused loopback TCP address.
func freeAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := l.Addr().String()
	require.NoError(t, l.Close())
	return addr
}

// postgresDSN returns the DSN of a fresh schema that is dropped when the
// test ends: on GARGI_POSTGRES_URL / POSTGRES_URL when set, else on an
// embedded PostgreSQL with its own runtime directory.
func postgresDSN(t *testing.T) (dsn, schema string) {
	t.Helper()
	base := os.Getenv("GARGI_POSTGRES_URL")
	if base == "" {
		base = os.Getenv("POSTGRES_URL")
	}
	if base == "" {
		addr := freeAddr(t)
		_, portStr, err := net.SplitHostPort(addr)
		require.NoError(t, err)
		var port uint32
		_, err = fmt.Sscan(portStr, &port)
		require.NoError(t, err)
		runtime := t.TempDir()
		db := embeddedpostgres.NewDatabase(embeddedpostgres.DefaultConfig().
			Port(port).
			Database("postgres").
			Username("postgres").
			Password("postgres").
			RuntimePath(runtime).
			DataPath(filepath.Join(runtime, "data")).
			Logger(io.Discard))
		require.NoError(t, db.Start(), "starting embedded-postgres")
		t.Cleanup(func() { _ = db.Stop() })
		base = fmt.Sprintf("postgres://postgres:postgres@127.0.0.1:%d/postgres?sslmode=disable", port)
	}

	ctx := context.Background()
	schema = fmt.Sprintf("srv_%d_%d", time.Now().UnixNano(), rand.Intn(100000))
	conn, err := pgx.Connect(ctx, base)
	require.NoError(t, err)
	_, err = conn.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize())
	require.NoError(t, err)
	require.NoError(t, conn.Close(ctx))
	t.Cleanup(func() {
		conn, err := pgx.Connect(context.Background(), base)
		if err != nil {
			return
		}
		_, _ = conn.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
		_ = conn.Close(context.Background())
	})

	u, err := url.Parse(base)
	require.NoError(t, err)
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	return u.String(), schema
}

// syncBuffer is a goroutine-safe log sink.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// server is one in-process run of the server.
type server struct {
	t    *testing.T
	base string
	logs *syncBuffer
	stop func()
}

// startServer runs the server on dsn with fresh ports and waits until it
// serves REST requests.
func startServer(t *testing.T, dsn string) *server {
	t.Helper()
	httpAddr, grpcAddr := freeAddr(t), freeAddr(t)
	cfg := fmt.Sprintf(`store:
  driver: postgres
  postgres:
    dsn: %q
temporal:
  host_port: %q
  namespace: default
  task_queue: gargi_tasks
server:
  http_address: %q
  grpc_address: %q
`, dsn, freeAddr(t), httpAddr, grpcAddr)
	cfgFile := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(cfgFile, []byte(cfg), 0o600))

	ctx, cancel := context.WithCancel(context.Background())
	var runErr error
	done := make(chan struct{})
	logs := &syncBuffer{}
	noEnv := func(string) (string, bool) { return "", false }
	go func() {
		defer close(done)
		runErr = run(ctx, []string{"-config", cfgFile}, noEnv, logs, io.Discard)
	}()

	var once sync.Once
	stop := func() {
		once.Do(func() {
			cancel()
			select {
			case <-done:
				require.NoError(t, runErr, "server run returned an error; logs:\n%s", logs.String())
			case <-time.After(60 * time.Second):
				t.Fatalf("server did not stop; logs:\n%s", logs.String())
			}
		})
	}
	t.Cleanup(func() {
		cancel()
		<-done
	})

	s := &server{t: t, base: "http://" + httpAddr, logs: logs, stop: stop}
	deadline := time.Now().Add(60 * time.Second)
	for {
		select {
		case <-done:
			t.Fatalf("server exited during startup: %v; logs:\n%s", runErr, logs.String())
		default:
		}
		resp, err := http.Get(s.base + "/v1/models")
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return s
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("server did not become ready; logs:\n%s", logs.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func (s *server) doRaw(method, path string, body any) (int, []byte) {
	s.t.Helper()
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		require.NoError(s.t, err)
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, s.base+path, reader)
	require.NoError(s.t, err)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(s.t, err)
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	require.NoError(s.t, err)
	return resp.StatusCode, raw
}

func (s *server) do(method, path string, body any) map[string]any {
	s.t.Helper()
	status, raw := s.doRaw(method, path, body)
	require.Equal(s.t, http.StatusOK, status, "%s %s: %s", method, path, raw)
	var out map[string]any
	require.NoError(s.t, json.Unmarshal(raw, &out), "body: %s", raw)
	return out
}

// followEvents reads the experiment's SSE stream until it ends and returns
// the event names and the data of the last event.
func (s *server) followEvents(id string) ([]string, map[string]any) {
	s.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.base+"/v1/experiments/"+id+"/events", nil)
	require.NoError(s.t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(s.t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(s.t, http.StatusOK, resp.StatusCode)
	var names []string
	var last map[string]any
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			names = append(names, strings.TrimPrefix(line, "event: "))
		case strings.HasPrefix(line, "data: "):
			last = nil
			require.NoError(s.t, json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &last))
		}
	}
	require.NoError(s.t, scanner.Err())
	return names, last
}

func summaryMeans(t *testing.T, raw []byte) []float64 {
	t.Helper()
	var report struct {
		Worlds []struct {
			Policies []struct {
				Objective struct {
					Mean float64 `json:"mean"`
				} `json:"objective"`
			} `json:"policies"`
		} `json:"worlds"`
	}
	require.NoError(t, json.Unmarshal(raw, &report), "%s", raw)
	means := make([]float64, len(report.Worlds))
	for i, w := range report.Worlds {
		require.NotEmpty(t, w.Policies)
		means[i] = w.Policies[0].Objective.Mean
	}
	return means
}

// TestRun_PostgresRestartKeepsExperiments is the #142 acceptance test: with
// store.driver postgres, models, experiments and reports survive a server
// restart, and experiments the first server left unfinished are failed.
func TestRun_PostgresRestartKeepsExperiments(t *testing.T) {
	dsn, schema := postgresDSN(t)

	// First server: save the loan model, run the exact experiment.
	s1 := startServer(t, dsn)
	require.Contains(t, s1.logs.String(), "Recovered interrupted simulation experiments")
	modelBody := map[string]any{
		"sim":      readLoan(t, "sim.yaml"),
		"graph":    readLoan(t, "graph.yaml"),
		"policies": map[string]string{"recession-v1.yaml": readLoan(t, filepath.Join("policies", "recession-v1.yaml"))},
		"format":   "yaml",
	}
	saved := s1.do(http.MethodPost, "/v1/models", modelBody)["model"].(map[string]any)
	require.Equal(t, "loan-underwriting", saved["id"])
	require.Equal(t, "1", saved["version"])

	experimentDoc := readLoan(t, "experiment.yaml")
	require.Contains(t, experimentDoc, "evaluator: both")
	experimentDoc = strings.Replace(experimentDoc, "evaluator: both", "evaluator: exact", 1)
	created := s1.do(http.MethodPost, "/v1/experiments", map[string]any{
		"model":      map[string]any{"id": "loan-underwriting"},
		"experiment": experimentDoc,
		"format":     "yaml",
	})
	id := created["id"].(string)
	require.NotEmpty(t, id)

	names, last := s1.followEvents(id)
	require.NotEmpty(t, names)
	require.Equal(t, "done", names[len(names)-1])
	require.Equal(t, "EXPERIMENT_STATUS_SUCCEEDED", last["status"], "%v", last)

	status, summaryBefore := s1.doRaw(http.MethodGet, "/v1/experiments/"+id+"/reports/summary", nil)
	require.Equal(t, http.StatusOK, status, "%s", summaryBefore)
	means := summaryMeans(t, summaryBefore)
	require.Len(t, means, 2)
	require.InDelta(t, 460.0, means[0], 1e-9)
	require.InDelta(t, 202.525, means[1], 1e-9)
	expBefore := s1.do(http.MethodGet, "/v1/experiments/"+id, nil)["experiment"]
	modelBefore := s1.do(http.MethodGet, "/v1/models/loan-underwriting", nil)["model"]
	runBefore := s1.do(http.MethodGet, "/v1/experiments/"+id+"/runs/0", nil)

	// Stop the first server and wait for run to return.
	s1.stop()

	// Simulate experiments a crashed process left behind: one running, one
	// queued.
	ctx := context.Background()
	pg, err := postgres.New(ctx, postgres.Config{ConnString: dsn})
	require.NoError(t, err)
	es := postgres.NewExperimentStore(pg)
	seeded := time.Now().UTC().Add(-time.Minute)
	for _, rec := range []sim.ExperimentRecord{
		{ID: "left-running", Status: sim.ExperimentRunning, StartedAt: seeded, Progress: sim.Progress{Done: 3, Total: 10}},
		{ID: "left-queued", Status: sim.ExperimentQueued},
	} {
		rec.Model = sim.ModelRef{ID: "loan-underwriting", Version: "1"}
		rec.Definition = []byte(experimentDoc)
		rec.Format = "yaml"
		rec.CreatedAt = seeded
		require.NoError(t, es.CreateExperiment(ctx, rec))
	}
	pg.Close()

	// Second server on the same database.
	s2 := startServer(t, dsn)
	require.Contains(t, s2.logs.String(), "count=2", "the restart recovered the two unfinished experiments; logs:\n%s", s2.logs.String())

	exp := s2.do(http.MethodGet, "/v1/experiments/"+id, nil)["experiment"].(map[string]any)
	require.Equal(t, "EXPERIMENT_STATUS_SUCCEEDED", exp["status"])
	require.Equal(t, expBefore, exp, "the experiment is unchanged by the restart")

	status, summaryAfter := s2.doRaw(http.MethodGet, "/v1/experiments/"+id+"/reports/summary", nil)
	require.Equal(t, http.StatusOK, status, "%s", summaryAfter)
	require.Equal(t, string(summaryBefore), string(summaryAfter), "the summary report is byte-identical after the restart")
	means = summaryMeans(t, summaryAfter)
	require.InDelta(t, 460.0, means[0], 1e-9)
	require.InDelta(t, 202.525, means[1], 1e-9)

	model := s2.do(http.MethodGet, "/v1/models/loan-underwriting", nil)["model"].(map[string]any)
	require.Equal(t, modelBefore, model)
	require.Equal(t, readLoan(t, "sim.yaml"), model["sim"])
	require.Equal(t, readLoan(t, "graph.yaml"), model["graph"])
	require.Equal(t, map[string]any{"recession-v1.yaml": readLoan(t, filepath.Join("policies", "recession-v1.yaml"))}, model["policies"])

	runAfter := s2.do(http.MethodGet, "/v1/experiments/"+id+"/runs/0", nil)
	require.NotEmpty(t, runAfter["events"])
	require.Equal(t, runBefore["record"], runAfter["record"], "replaying run 0 works after the restart")

	for _, left := range []string{"left-running", "left-queued"} {
		got := s2.do(http.MethodGet, "/v1/experiments/"+left, nil)["experiment"].(map[string]any)
		require.Equal(t, "EXPERIMENT_STATUS_FAILED", got["status"], "%s: %v", left, got)
		require.Equal(t, jobs.MsgInterrupted, got["error"], "%s: %v", left, got)
	}

	// Versioning continues on the persisted models.
	again := s2.do(http.MethodPost, "/v1/models", modelBody)["model"].(map[string]any)
	require.Equal(t, "2", again["version"])

	// The tables live in the configured schema.
	conn, err := pgx.Connect(ctx, dsn)
	require.NoError(t, err)
	defer func() { _ = conn.Close(ctx) }()
	var n int
	require.NoError(t, conn.QueryRow(ctx, "SELECT count(*) FROM "+pgx.Identifier{schema, "sim_experiments"}.Sanitize()).Scan(&n))
	require.Equal(t, 3, n)
}
