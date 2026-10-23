package kernel

import (
	"context"
	"fmt"
	"time"

	"github.com/gargi-indic/gargi-flow/internal/nodes"
	"github.com/gargi-indic/gargi-flow/internal/reactive"
	"github.com/gargi-indic/gargi-flow/internal/sim/behavior"
	"github.com/gargi-indic/gargi-flow/internal/sim/model"
	"github.com/gargi-indic/gargi-flow/pkg/engine"
	"github.com/gargi-indic/gargi-flow/pkg/sim"
)

// Options configuration for the episode run.
type Options struct {
	// Events is an optional sink for reactive engine events.
	Events engine.EventSink
}

type recordingChooser struct {
	policy    sim.Policy
	decisions map[engine.NodeID]string
}

func (r *recordingChooser) Choose(ctx context.Context, decision engine.NodeID, obs map[string]any, options []string) (string, error) {
	choice, err := r.policy.Choose(ctx, decision, obs, options)
	if err != nil {
		return "", err
	}
	isValid := false
	for _, opt := range options {
		if opt == choice {
			isValid = true
			break
		}
	}
	if !isValid {
		return "", engine.NewNonRetryableError(fmt.Errorf("policy returned invalid choice %q", choice))
	}
	if r.decisions == nil {
		r.decisions = make(map[engine.NodeID]string)
	}
	r.decisions[decision] = choice
	return choice, nil
}

type runNodeContext struct {
	execID engine.ExecutionID
	nodeID engine.NodeID
	config map[string]any
	inputs map[string]any
	state  *engine.GraphState
}

func (c *runNodeContext) ExecutionID() engine.ExecutionID { return c.execID }
func (c *runNodeContext) NodeID() engine.NodeID           { return c.nodeID }
func (c *runNodeContext) Config() map[string]any          { return c.config }
func (c *runNodeContext) Inputs() map[string]any          { return c.inputs }
func (c *runNodeContext) State() *engine.GraphState       { return c.state }
func (c *runNodeContext) Attempt() int                    { return 1 }

// RunEpisode runs one episode of the simulated model.
func RunEpisode(ctx context.Context, m *model.CompiledModel, w sim.World, p sim.Policy, s sim.Sampler, opts Options) (sim.RunRecord, error) {
	execID, err := engine.NewExecutionID()
	if err != nil {
		return sim.RunRecord{}, err
	}

	record := sim.RunRecord{
		World:     w.ID,
		PolicyID:  p.ID(),
		Decisions: make(map[engine.NodeID]string),
	}

	vars := map[string]any{
		"world": w.Params,
	}

	latents, err := behavior.SampleLatents(m, s, vars)
	if err != nil {
		return sim.RunRecord{}, err
	}
	vars["latent"] = latents

	reg := engine.NewRegistry()
	if err := nodes.RegisterDefaults(reg, nodes.Dependencies{}); err != nil {
		return sim.RunRecord{}, err
	}
	chooser := &recordingChooser{
		policy:    p,
		decisions: record.Decisions,
	}
	if err := nodes.RegisterDecision(reg, chooser); err != nil {
		return sim.RunRecord{}, err
	}

	var virtualClock = time.Unix(0, 0).UTC()
	clockFn := func() time.Time { return virtualClock }

	seqGen := reactive.NewSequenceGenerator(0)
	emitter := reactive.NewTransitionEmitter(execID, seqGen, clockFn, opts.Events)
	resolver := reactive.NewPreconditionResolver(nil)
	g, err := m.Graph.ToGraph()
	if err != nil {
		return sim.RunRecord{}, err
	}
	scheduler := reactive.NewScheduler(g, reg, resolver, emitter)
	detector := reactive.NewTerminalDetector(g, m.Graph.FailurePolicy, emitter)

	state, err := engine.NewGraphState(execID, g)
	if err != nil {
		return sim.RunRecord{}, err
	}

	heap := &virtualTimeHeap{}

	nodeDefs := g.Nodes()
	nodeDefMap := make(map[engine.NodeID]*engine.Node)
	for _, n := range nodeDefs {
		nodeDefMap[n.ID()] = n
	}

	for {
		plan, nextState, err := scheduler.EvaluateStep(ctx, state)
		if err != nil {
			return sim.RunRecord{}, err
		}
		state = nextState

		for _, nodeID := range plan.ReadyNodes {
			oldNS := state.NodeState(nodeID)
			statusRunning := engine.StatusRunning
			mutRunning := engine.StateMutation{NodeID: nodeID, ExpectedVersion: state.Version(), Status: &statusRunning}
			state, err = state.Apply(mutRunning)
			if err != nil {
				return sim.RunRecord{}, err
			}
			_, _ = emitter.EmitTransition(ctx, oldNS, state.NodeState(nodeID), mutRunning)

			nodeDef := nodeDefMap[nodeID]
			inputs := plan.ResolvedInputs[nodeID]

			strID := string(nodeID)
			if cb, ok := m.CompiledBehaviors[strID]; ok {
				// Simulated Behavior
				failClass := ""
				if defNode, defOk := m.Def.Behaviors[strID]; defOk && defNode.Fail != nil {
					failClass = defNode.Fail.Class
				}
				b := behavior.New(cb, failClass)
				in := sim.BehaviorInput{
					NodeID:  nodeID,
					Inputs:  inputs,
					Latents: latents,
					World:   w,
				}
				out, err := b.Sample(ctx, s, in)
				if err != nil {
					return sim.RunRecord{}, err
				}
				if out.Abandoned {
					record.Abandoned = true
					record.Duration = virtualClock.Sub(time.Unix(0, 0).UTC())
					record.FinalStatus = engine.StatusFailed
					return record, nil
				}

				record.Cost += out.Cost

				// Schedule completion
				completionTime := virtualClock.Add(out.Duration)
				heap.push(scheduledEvent{time: completionTime, nodeID: nodeID, outcome: out})
			} else {
				// Real Execution
				instance, err := reg.Instantiate(nodeDef.Type(), nodeDef.Config())
				if err != nil {
					return sim.RunRecord{}, err
				}
				nodeCtx := &runNodeContext{
					execID: execID,
					nodeID: nodeID,
					config: nodeDef.Config(),
					inputs: inputs,
					state:  state,
				}
				err = instance.OnInit(ctx, nodeCtx)
				if err != nil {
					_ = instance.OnFailure(ctx, nodeCtx, err)
					heap.push(scheduledEvent{time: virtualClock, nodeID: nodeID, outcome: sim.BehaviorOutcome{Err: err}})
					continue
				}
				mut, err := instance.Execute(ctx, nodeCtx)
				if err != nil {
					_ = instance.OnFailure(ctx, nodeCtx, err)
					heap.push(scheduledEvent{time: virtualClock, nodeID: nodeID, outcome: sim.BehaviorOutcome{Err: err}})
					continue
				}
				_ = instance.OnSuccess(ctx, nodeCtx, mut)

				var output map[string]any
				if mut != nil {
					output = mut.Set
				}
				heap.push(scheduledEvent{time: virtualClock, nodeID: nodeID, outcome: sim.BehaviorOutcome{Output: output}})
			}
		}

		if len(plan.ReadyNodes) == 0 && len(plan.SkippedNodes) == 0 {
			if heap.Len() == 0 {
				assessment, err := detector.AssessState(ctx, state)
				if err != nil {
					return sim.RunRecord{}, err
				}
				if assessment.Phase != reactive.PhaseRunning {
					state = assessment.FinalGraphState
					break
				}
				break
			}
		}

		if heap.Len() > 0 {
			ev := heap.pop()
			virtualClock = ev.time

			oldNS := state.NodeState(ev.nodeID)

			if ev.outcome.Err != nil {
				cause := ev.outcome.Err
				if !engine.IsCritical(cause) {
					cause = engine.NewNonRetryableError(cause)
				}
				detector.RecordNodeFailure(ev.nodeID, cause)

				statusFailed := engine.StatusFailed
				errMsg := cause.Error()
				mut := engine.StateMutation{
					NodeID:          ev.nodeID,
					ExpectedVersion: state.Version(),
					Status:          &statusFailed,
					ErrorMessage:    &errMsg,
				}
				state, err = state.Apply(mut)
				if err != nil {
					return sim.RunRecord{}, err
				}
				_, _ = emitter.EmitTransition(ctx, oldNS, state.NodeState(ev.nodeID), mut, cause)
			} else {
				statusSucceeded := engine.StatusSucceeded
				mut := engine.StateMutation{
					NodeID:          ev.nodeID,
					ExpectedVersion: state.Version(),
					Status:          &statusSucceeded,
					Set:             ev.outcome.Output,
				}
				state, err = state.Apply(mut)
				if err != nil {
					return sim.RunRecord{}, err
				}
				_, _ = emitter.EmitTransition(ctx, oldNS, state.NodeState(ev.nodeID), mut)
				record.Path = append(record.Path, ev.nodeID)
			}

			assessment, err := detector.AssessState(ctx, state)
			if err != nil {
				return sim.RunRecord{}, err
			}
			if assessment.Phase != reactive.PhaseRunning {
				state = assessment.FinalGraphState
				break
			}
		}
	}

	record.Duration = virtualClock.Sub(time.Unix(0, 0).UTC())
	record.FinalStatus = state.Status()

	nodesMap := make(map[string]any)
	for _, ns := range state.NodeStates() {
		nID := string(ns.NodeID())
		m := make(map[string]any)
		m["status"] = string(ns.Status())
		m["output"] = ns.Output()
		m["errorMessage"] = ns.ErrorMessage()
		for k, v := range ns.Output() {
			if _, exists := m[k]; !exists {
				m[k] = v
			}
		}
		nodesMap[nID] = m
	}
	vars["nodes"] = nodesMap

	runVars := map[string]any{
		"cost":     record.Cost,
		"duration": record.Duration.Seconds(),
		"path":     []string{},
	}
	var pathStrs []string
	for _, id := range record.Path {
		pathStrs = append(pathStrs, string(id))
	}
	runVars["path"] = pathStrs
	vars["run"] = runVars

	if m.CompiledObjective != nil {
		val, _, err := m.CompiledObjective.Eval(vars)
		if err != nil {
			return sim.RunRecord{}, fmt.Errorf("objective eval error: %w", err)
		}
		switch v := val.Value().(type) {
		case float64:
			record.Objective = v
		case int:
			record.Objective = float64(v)
		case int64:
			record.Objective = float64(v)
		default:
			return sim.RunRecord{}, fmt.Errorf("objective must evaluate to a number, got %T", val.Value())
		}
	}

	record.Constraints = make(map[string]bool)
	for name, prog := range m.CompiledConstraints {
		val, _, err := prog.Eval(vars)
		if err != nil {
			return sim.RunRecord{}, fmt.Errorf("constraint %s eval error: %w", name, err)
		}
		if b, ok := val.Value().(bool); ok {
			record.Constraints[name] = b
		} else {
			return sim.RunRecord{}, fmt.Errorf("constraint %s must evaluate to bool, got %T", name, val.Value())
		}
	}

	return record, nil
}
