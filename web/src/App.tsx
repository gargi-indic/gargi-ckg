import { useState, useMemo, useRef, useEffect } from 'react';
import * as jsyaml from 'js-yaml';
import type { Node as ReactFlowNode, Edge } from '@xyflow/react';
import './App.css';
import GraphCanvas from './components/GraphCanvas';
import { apiClient } from './api/client';
import type { definitions } from './api/schema';

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

export type EventLog = {
  timestamp: string;
  nodeId: string;
  fromStatus: string;
  toStatus: string;
};

function App() {
  const [inputData, setInputData] = useState<string>('');
  const [fetchError, setFetchError] = useState<string | null>(null);

  // Execution state
  const [executionId, setExecutionId] = useState<string | null>(null);
  const [nodeStatuses, setNodeStatuses] = useState<Record<string, string>>({});
  const [eventLogs, setEventLogs] = useState<EventLog[]>([]);
  const [isRunning, setIsRunning] = useState(false);
  const eventSourceRef = useRef<EventSource | null>(null);
  const pollIntervalRef = useRef<number | null>(null);

  const handleLoadExample = async (path: string) => {
    if (!path) return;
    try {
      const res = await fetch(path);
      const text = await res.text();
      setInputData(text);
      setFetchError(null);
      resetExecution();
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
      resetExecution();
    };
    reader.onerror = () => {
      setFetchError('Failed to read file');
    };
    reader.readAsText(file);
  };

  const resetExecution = () => {
    setExecutionId(null);
    setNodeStatuses({});
    setEventLogs([]);
    setIsRunning(false);
    if (eventSourceRef.current) {
      eventSourceRef.current.close();
      eventSourceRef.current = null;
    }
    if (pollIntervalRef.current) {
      clearInterval(pollIntervalRef.current);
      pollIntervalRef.current = null;
    }
  };

  useEffect(() => {
    return () => {
      resetExecution();
    };
  }, []);

  const handleRunExecution = async () => {
    if (!inputData.trim()) return;
    resetExecution();
    setIsRunning(true);

    const newExecId = crypto.randomUUID();
    setExecutionId(newExecId);

    // Open EventSource before POST
    const baseUrl = import.meta.env.VITE_API_BASE_URL || 'http://localhost:8080';
    const eventSource = new EventSource(`${baseUrl}/v1/executions/${newExecId}/events`);
    eventSourceRef.current = eventSource;

    eventSource.onmessage = (event) => {
      try {
        const data = JSON.parse(event.data) as definitions["v1StreamExecutionUpdatesResponse"];

        if (data.nodeId && data.toStatus) {
          setNodeStatuses(prev => ({
            ...prev,
            [data.nodeId!]: data.toStatus!
          }));

          setEventLogs(prev => {
            const newLog: EventLog = {
              timestamp: data.timestamp || new Date().toISOString(),
              nodeId: data.nodeId!,
              fromStatus: data.fromStatus || '',
              toStatus: data.toStatus!
            };
            // Keep last 100 logs
            return [newLog, ...prev].slice(0, 100);
          });
        }
      } catch (err) {
        console.error('Failed to parse SSE message', err);
      }
    };

    eventSource.onerror = () => {
      console.warn('SSE Error, falling back to polling');
      eventSource.close();
      eventSourceRef.current = null;
      startPolling(newExecId);
    };

    try {
      // Workaround for openapi-fetch types mismatch when generated with swagger 2.0
      const res = await apiClient.POST('/v1/executions', {
        body: {
          executionId: newExecId,
          graphDefinition: inputData,
          wait: false,
        }
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      } as any);

      if (res.error) {
        // eslint-disable-next-line @typescript-eslint/no-explicit-any
        const errObj = res.error as any;
        setFetchError(`Execution failed: ${errObj.message || JSON.stringify(res.error)}`);
        setIsRunning(false);
        resetExecution();
      }
    } catch (err) {
      setFetchError(`Execution submission error: ${err instanceof Error ? err.message : 'Unknown'}`);
      setIsRunning(false);
      resetExecution();
    }
  };

  const startPolling = (execId: string) => {
    if (pollIntervalRef.current) {
      clearInterval(pollIntervalRef.current);
    }

    pollIntervalRef.current = window.setInterval(async () => {
      try {
        const res = await apiClient.GET('/v1/executions/{executionId}', {
          params: { path: { executionId: execId } }
        });

        if (res.data && typeof res.data === 'object' && 'nodeStates' in res.data) {
          // eslint-disable-next-line @typescript-eslint/no-explicit-any
          const dataAny = res.data as any;
          if (dataAny.nodeStates) {
            const newStatuses: Record<string, string> = {};
            let statusChanged = false;

            // eslint-disable-next-line @typescript-eslint/no-explicit-any
            Object.values(dataAny.nodeStates).forEach((state: any) => {
              if (state.nodeId && state.status) {
                newStatuses[state.nodeId] = state.status;
              }
            });

          const logsToAdd: EventLog[] = [];
          setNodeStatuses(prev => {
            const updated = { ...prev };
            Object.entries(newStatuses).forEach(([nodeId, status]) => {
              if (prev[nodeId] !== status) {
                statusChanged = true;
                updated[nodeId] = status;
                logsToAdd.push({
                  timestamp: new Date().toISOString(),
                  nodeId,
                  fromStatus: prev[nodeId] || '',
                  toStatus: status
                });
              }
            });
            return statusChanged ? updated : prev;
          });

          if (logsToAdd.length > 0) {
            setEventLogs(logs => {
              return [...logsToAdd, ...logs].slice(0, 100);
            });
          }

            // Stop polling if execution is terminal
            const execStatus = dataAny.status;
            if (execStatus === 'EXECUTION_STATUS_SUCCEEDED' ||
                execStatus === 'EXECUTION_STATUS_FAILED' ||
                execStatus === 'EXECUTION_STATUS_CANCELLED') {
              clearInterval(pollIntervalRef.current!);
              pollIntervalRef.current = null;
              setIsRunning(false);
            }
          }
        }
      } catch (err) {
        console.error('Polling error', err);
      }
    }, 1000);
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
          status: nodeStatuses[node.id] || 'NODE_STATUS_UNSPECIFIED',
        },
      }));

      const edgesArr = Array.isArray(parsedObj.edges) ? (parsedObj.edges as EdgeDef[]) : [];
      const reactFlowEdges: Edge[] = edgesArr.map((edge, index) => {
        const sourceStatus = nodeStatuses[edge.from];
        const isDimmed = sourceStatus === 'NODE_STATUS_SKIPPED';
        const isAnimated = sourceStatus === 'NODE_STATUS_SUCCEEDED';

        return {
          id: `e-${edge.from}-${edge.to}-${index}`,
          source: edge.from,
          target: edge.to,
          label: edge.condition || '',
          animated: isAnimated,
          style: {
            stroke: isDimmed ? '#ddd' : '#888',
            opacity: isDimmed ? 0.3 : 1,
            strokeDasharray: isDimmed ? '5,5' : 'none',
          },
        };
      });

      return { nodes: reactFlowNodes, edges: reactFlowEdges, error: null };
    } catch (err: unknown) {
      if (err instanceof Error) {
        return { nodes: [], edges: [], error: `Parse/Validation Error: ${err.message}` };
      }
      return { nodes: [], edges: [], error: 'Parse/Validation Error: Unknown error' };
    }
  }, [inputData, nodeStatuses]);

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
        <button
          onClick={handleRunExecution}
          disabled={!inputData.trim() || !!parsedGraphResult.error || isRunning}
          style={{
            padding: '8px 16px',
            backgroundColor: (isRunning || !inputData.trim() || !!parsedGraphResult.error) ? '#ccc' : '#4caf50',
            color: 'white',
            border: 'none',
            borderRadius: '4px',
            cursor: (isRunning || !inputData.trim() || !!parsedGraphResult.error) ? 'not-allowed' : 'pointer',
            fontWeight: 'bold'
          }}
        >
          {isRunning ? 'Running...' : 'Run'}
        </button>
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

          {executionId && (
            <div style={{ borderTop: '1px solid #ccc', height: '200px', display: 'flex', flexDirection: 'column' }}>
              <div style={{ padding: '10px', background: '#eee', fontWeight: 'bold', fontSize: '12px' }}>
                Live Events: {executionId}
              </div>
              <div style={{ flex: 1, overflowY: 'auto', padding: '10px', background: '#fafafa', fontSize: '11px', fontFamily: 'monospace' }}>
                {eventLogs.map((log, i) => (
                  <div key={i} style={{ marginBottom: '4px', borderBottom: '1px solid #eee', paddingBottom: '2px' }}>
                    <span style={{ color: '#888' }}>[{new Date(log.timestamp).toLocaleTimeString()}]</span>{' '}
                    <strong>{log.nodeId}</strong>: {log.fromStatus.replace('NODE_STATUS_', '')} → <span style={{ color: '#0066cc' }}>{log.toStatus.replace('NODE_STATUS_', '')}</span>
                  </div>
                ))}
                {eventLogs.length === 0 && <div style={{ color: '#888', fontStyle: 'italic' }}>Waiting for events...</div>}
              </div>
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
