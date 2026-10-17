with open("internal/orchestrator/activity_node_test.go", "r") as f:
    text = f.read()

text = text.replace("func (m *mockNode) OnSuccess(ctx context.Context, nc engine.NodeContext, result *engine.StateMutation)", "func (m *mockNode) OnSuccess(_ context.Context, _ engine.NodeContext, _ *engine.StateMutation)")
text = text.replace("func (m *mockNode) OnFailure(ctx context.Context, nc engine.NodeContext, cause error)", "func (m *mockNode) OnFailure(_ context.Context, _ engine.NodeContext, _ error)")

with open("internal/orchestrator/activity_node_test.go", "w") as f:
    f.write(text)
