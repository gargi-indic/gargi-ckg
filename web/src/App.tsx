import { useState, useMemo } from 'react';
import * as jsyaml from 'js-yaml';
import type { Node as ReactFlowNode, Edge } from '@xyflow/react';
import './App.css';
import GraphCanvas from './components/GraphCanvas';

const EXAMPLES = [
  { label: 'Conditional Branch (JSON)', path: '/examples/conditional_branch.json' },
  { label: 'Conditional Branch (YAML)', path: '/examples/conditional_branch.yaml' },
  { label: 'Fanout Fanin (JSON)', path: '/examples/fanout_fanin.json' },
  { label: 'Fanout Fanin (YAML)', path: '/examples/fanout_fanin.yaml' },
  { label: 'Linear Pipeline (JSON)', path: '/examples/linear_pipeline.json' },
  { label: 'Linear Pipeline (YAML)', path: '/examples/linear_pipeline.yaml' },
  { label: 'Saga Compensation (YAML)', path: '/examples/saga_compensation.yaml' },
  { label: 'Wait Signal (YAML)', path: '/examples/wait_signal.yaml' },
];

function App() {
  const [inputData, setInputData] = useState<string>('');
  const [fetchError, setFetchError] = useState<string | null>(null);

  const handleLoadExample = async (path: string) => {
    if (!path) return;
    try {
      const res = await fetch(path);
      const text = await res.text();
      setInputData(text);
      setFetchError(null);
    } catch (err: unknown) {
      if (err instanceof Error) {
        setFetchError(`Failed to load example: ${err.message}`);
      } else {
        setFetchError('Failed to load example: Unknown error');
      }
    }
  };

  const handleFileUpload = (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (!file) return;
    const reader = new FileReader();
    reader.onload = (event) => {
      setInputData(event.target?.result as string);
      setFetchError(null);
    };
    reader.onerror = () => {
      setFetchError('Failed to read file');
    };
    reader.readAsText(file);
  };

  const parsedGraphResult = useMemo(() => {
    if (!inputData.trim()) {
      return { nodes: [], edges: [], error: null };
    }

    try {
      let parsed: unknown;
      try {
        parsed = JSON.parse(inputData);
      } catch {
        // Fallback to YAML
        parsed = jsyaml.load(inputData);
      }

      if (!parsed || typeof parsed !== 'object') {
        throw new Error('Parsed data is not a valid object');
      }

      const parsedObj = parsed as Record<string, unknown>;

      if (!Array.isArray(parsedObj.nodes)) {
        throw new Error('Graph definition must contain a "nodes" array');
      }

      type NodeDef = { id: string; type?: string; name?: string };
      type EdgeDef = { from: string; to: string; condition?: string };

      const reactFlowNodes: ReactFlowNode[] = (parsedObj.nodes as NodeDef[]).map((node) => ({
        id: node.id,
        type: 'custom',
        position: { x: 0, y: 0 }, // Dagre will position
        data: {
          id: node.id,
          type: node.type || 'unknown',
          label: node.name || node.id,
        },
      }));

      const edgesArr = Array.isArray(parsedObj.edges) ? (parsedObj.edges as EdgeDef[]) : [];
      const reactFlowEdges: Edge[] = edgesArr.map((edge, index) => ({
        id: `e-${edge.from}-${edge.to}-${index}`,
        source: edge.from,
        target: edge.to,
        label: edge.condition || '',
        animated: false,
        style: { stroke: '#888' },
      }));

      return { nodes: reactFlowNodes, edges: reactFlowEdges, error: null };
    } catch (err: unknown) {
      if (err instanceof Error) {
        return { nodes: [], edges: [], error: `Parse/Validation Error: ${err.message}` };
      }
      return { nodes: [], edges: [], error: 'Parse/Validation Error: Unknown error' };
    }
  }, [inputData]);

  const displayError = fetchError || parsedGraphResult.error;

  return (
    <div className="App" style={{ display: 'flex', flexDirection: 'column', height: '100vh' }}>
      <header style={{ padding: '10px 20px', background: '#282c34', color: 'white', display: 'flex', gap: '20px', alignItems: 'center' }}>
        <h1 style={{ margin: 0, fontSize: '1.5rem' }}>Gargi Flow</h1>
        <select onChange={(e) => handleLoadExample(e.target.value)} defaultValue="">
          <option value="" disabled>Load Example...</option>
          {EXAMPLES.map(ex => (
            <option key={ex.path} value={ex.path}>{ex.label}</option>
          ))}
        </select>
        <input type="file" accept=".json,.yaml,.yml" onChange={handleFileUpload} style={{ color: 'white' }} />
      </header>

      <div style={{ display: 'flex', flex: 1, overflow: 'hidden' }}>
        <div style={{ width: '30%', minWidth: '300px', borderRight: '1px solid #ccc', display: 'flex', flexDirection: 'column' }}>
          <div style={{ padding: '10px', background: '#eee', fontWeight: 'bold' }}>Input (JSON/YAML)</div>
          <textarea
            style={{ flex: 1, width: '100%', padding: '10px', boxSizing: 'border-box', fontFamily: 'monospace', resize: 'none' }}
            value={inputData}
            onChange={(e) => setInputData(e.target.value)}
            placeholder="Paste GraphDefinition JSON or YAML here..."
          />
          {displayError && (
            <div style={{ padding: '10px', color: 'red', background: '#ffebee', borderTop: '1px solid #ffcdd2' }}>
              {displayError}
            </div>
          )}
        </div>

        <div style={{ flex: 1, position: 'relative' }}>
          {parsedGraphResult.nodes.length > 0 ? (
            <GraphCanvas nodes={parsedGraphResult.nodes} edges={parsedGraphResult.edges} />
          ) : (
            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '100%', color: '#888' }}>
              No graph to display
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

export default App;
