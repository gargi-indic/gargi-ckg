package orchestrator

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/gargi-indic/gargi-ckg/internal/config"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
)

// WorkerManager oversees workflow and activity workers with configurable task queues.
type WorkerManager struct {
	client         client.Client
	workflowWorker worker.Worker
	activityWorker worker.Worker
	sharedWorker   bool
	cfg            config.TemporalConfig
	logger         *slog.Logger
}

// NewWorkerManager creates a new WorkerManager for the given client and TemporalConfig.
func NewWorkerManager(
	c client.Client,
	cfg config.TemporalConfig,
	logger *slog.Logger,
) *WorkerManager {
	wfQueue := cfg.EffectiveWorkflowTaskQueue()
	actQueue := cfg.EffectiveActivityTaskQueue()

	wm := &WorkerManager{
		client: c,
		cfg:    cfg,
		logger: logger,
	}

	workerOpts := worker.Options{
		BuildID:                 cfg.BuildID,
		UseBuildIDForVersioning: cfg.UseBuildIDForVersioning,
	}

	if wfQueue == actQueue {
		w := worker.New(c, wfQueue, workerOpts)
		wm.workflowWorker = w
		wm.activityWorker = w
		wm.sharedWorker = true
	} else {
		wm.workflowWorker = worker.New(c, wfQueue, workerOpts)
		wm.activityWorker = worker.New(c, actQueue, workerOpts)
		wm.sharedWorker = false
	}

	return wm
}

// WorkflowTaskQueue returns the effective workflow task queue name.
func (m *WorkerManager) WorkflowTaskQueue() string {
	return m.cfg.EffectiveWorkflowTaskQueue()
}

// ActivityTaskQueue returns the effective activity task queue name.
func (m *WorkerManager) ActivityTaskQueue() string {
	return m.cfg.EffectiveActivityTaskQueue()
}

// RegisterWorkflow registers a workflow function with the workflow worker.
func (m *WorkerManager) RegisterWorkflow(w any) {
	m.workflowWorker.RegisterWorkflow(w)
}

// RegisterWorkflowWithOptions registers a workflow function with options.
func (m *WorkerManager) RegisterWorkflowWithOptions(w any, options workflow.RegisterOptions) {
	m.workflowWorker.RegisterWorkflowWithOptions(w, options)
}

// RegisterActivity registers an activity struct or function with the activity worker.
func (m *WorkerManager) RegisterActivity(a any) {
	m.activityWorker.RegisterActivity(a)
}

// RegisterActivityWithOptions registers an activity struct or function with options.
func (m *WorkerManager) RegisterActivityWithOptions(a any, options activity.RegisterOptions) {
	m.activityWorker.RegisterActivityWithOptions(a, options)
}

// Start non-blockingly starts the underlying worker(s).
func (m *WorkerManager) Start() error {
	if m.sharedWorker {
		return m.workflowWorker.Start()
	}

	if err := m.workflowWorker.Start(); err != nil {
		return fmt.Errorf("starting workflow worker: %w", err)
	}

	if err := m.activityWorker.Start(); err != nil {
		m.workflowWorker.Stop()
		return fmt.Errorf("starting activity worker: %w", err)
	}

	return nil
}

// Run starts the worker(s), blocks until ctx.Done() is closed, and stops the worker(s) gracefully.
func (m *WorkerManager) Run(ctx context.Context) error {
	if err := m.Start(); err != nil {
		return err
	}

	<-ctx.Done()

	m.Stop()

	return nil
}

// Stop stops the underlying worker(s) gracefully.
func (m *WorkerManager) Stop() {
	if m.sharedWorker {
		m.workflowWorker.Stop()
		return
	}

	m.workflowWorker.Stop()
	m.activityWorker.Stop()
}
