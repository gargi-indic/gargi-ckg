package nodes

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/gargi-indic/gargi-flow/pkg/engine"
)

// DecisionChooser represents a strategy for making decisions.
type DecisionChooser interface {
	Choose(ctx context.Context, decision engine.NodeID, obs map[string]any, options []string) (string, error)
}

type decisionConfig struct {
	Options  []string `json:"options"`
	Observes []string `json:"observes"`
	Policy   string   `json:"policy,omitempty"`
}

func (c *decisionConfig) Validate() error {
	if len(c.Options) == 0 {
		return errors.New("at least one option is required")
	}

	seenOptions := make(map[string]struct{})
	for _, opt := range c.Options {
		if _, exists := seenOptions[opt]; exists {
			return fmt.Errorf("duplicate option: %s", opt)
		}
		seenOptions[opt] = struct{}{}
	}

	for _, obs := range c.Observes {
		parts := strings.Split(obs, ".")
		if len(parts) < 2 {
			return fmt.Errorf("invalid observe path %q: must be <nodeId>.output.<field> or <nodeId>.status", obs)
		}

		switch parts[1] {
		case "status":
			if len(parts) != 2 {
				return fmt.Errorf("invalid observe path %q: status paths must be exactly <nodeId>.status", obs)
			}
		case "output":
			if len(parts) < 3 {
				return fmt.Errorf("invalid observe path %q: output paths must be <nodeId>.output.<field>", obs)
			}
		default:
			return fmt.Errorf("invalid observe path %q: must be <nodeId>.output.<field> or <nodeId>.status", obs)
		}
	}

	return nil
}

type decisionNode struct {
	*engine.BaseNode
	cfg     decisionConfig
	chooser DecisionChooser
}

var _ engine.NodeInterface = (*decisionNode)(nil)

func (n *decisionNode) Execute(ctx context.Context, nc engine.NodeContext) (*engine.StateMutation, error) {
	state := nc.State()
	obs := make(map[string]any)

	for _, path := range n.cfg.Observes {
		parts := strings.Split(path, ".")
		nodeID := engine.NodeID(parts[0])
		nodeState := state.NodeState(nodeID)

		if nodeState == nil {
			obs[path] = nil
			continue
		}

		switch parts[1] {
		case "status":
			obs[path] = string(nodeState.Status())
		case "output":
			if nodeState.Status() == engine.StatusSucceeded {
				outputs := nodeState.Output()
				fieldPath := strings.Join(parts[2:], ".")
				if val, exists := outputs[fieldPath]; exists {
					obs[path] = val
				} else {
					obs[path] = nil
				}
			} else {
				obs[path] = nil
			}
		}
	}

	choice, err := n.chooser.Choose(ctx, nc.NodeID(), obs, n.cfg.Options)
	if err != nil {
		return nil, engine.NewNonRetryableError(fmt.Errorf("chooser failed: %w", err))
	}

	isValidChoice := false
	for _, opt := range n.cfg.Options {
		if opt == choice {
			isValidChoice = true
			break
		}
	}

	if !isValidChoice {
		return nil, engine.NewNonRetryableError(fmt.Errorf("chooser returned invalid option %q", choice))
	}

	mut := engine.NewStateMutation(nc.NodeID(), 0, nil, map[string]any{
		"choice": choice,
	}, nil, nil)

	return &mut, nil
}

// RegisterDecision registers the decision node type.
func RegisterDecision(reg *engine.Registry, chooser DecisionChooser) error {
	if reg == nil {
		return errors.New("registry cannot be nil")
	}
	if chooser == nil {
		return errors.New("chooser cannot be nil")
	}

	return reg.Register("decision", func(config any) (engine.NodeInterface, error) {
		return &decisionNode{
			BaseNode: &engine.BaseNode{},
			cfg:      *config.(*decisionConfig),
			chooser:  chooser,
		}, nil
	}, &decisionConfig{})
}
