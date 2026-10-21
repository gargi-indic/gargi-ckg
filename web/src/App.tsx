import { useState, useMemo, useRef, useEffect, useCallback } from 'react';
import * as jsyaml from 'js-yaml';
import {
  addEdge,
  applyNodeChanges,
  applyEdgeChanges,
  useReactFlow,
  ReactFlowProvider
} from '@xyflow/react';
import type {
  Node as ReactFlowNode,
  Edge,
  Connection,
  EdgeChange,
  NodeChange,
} from '@xyflow/react';
import dagre from 'dagre';
import './App.css';
import GraphCanvas from './components/GraphCanvas';
import NodeDrawer from './components/NodeDrawer';
import EdgeDrawer from './components/EdgeDrawer';
import { ExecutionsSidebar } from './components/ExecutionsSidebar';
import { ExperimentsGallery } from './components/ExperimentsGallery';
import { NodePalette } from './components/NodePalette';
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

const nodeWidth = 200;
const nodeHeight = 100;

const getLayoutedElements = (nodes: ReactFlowNode[], edges: Edge[], direction = 'TB') => {
  const dagreGraph = new dagre.graphlib.Graph();
  dagreGraph.setDefaultEdgeLabel(() => ({}));

  // Increase nodesep and ranksep to give space for edge labels
  dagreGraph.setGraph({ rankdir: direction, nodesep: 150, ranksep: 100 });

  nodes.forEach((node) => {
    dagreGraph.setNode(node.id, { width: nodeWidth, height: nodeHeight });
  });

  edges.forEach((edge) => {
    dagreGraph.setEdge(edge.source, edge.target);
  });

  dagre.layout(dagreGraph);

  const layoutedNodes = nodes.map((node) => {
    const nodeWithPosition = dagreGraph.node(node.id);
    // Adjust to shift the node center to top left for react flow
    return {
      ...node,
      position: {
        x: nodeWithPosition.x - nodeWidth / 2,
        y: nodeWithPosition.y - nodeHeight / 2,
      },
    };
  });

  return { layoutedNodes, layoutedEdges: edges };
};

function AppFlow() {
  const [inputData, setInputData] = useState<string>('');
  const [fetchError, setFetchError] = useState<string | null>(null);

  // Execution state
  const [executionId, setExecutionId] = useState<string | null>(null);
  const [nodeStatuses, setNodeStatuses] = useState<Record<string, string>>({});
  const [eventLogs, setEventLogs] = useState<EventLog[]>([]);
  const [isRunning, setIsRunning] = useState(false);
  const eventSourceRef = useRef<EventSource | null>(null);
  const pollIntervalRef = useRef<number | null>(null);

  // Drawer state
  const [selectedNodeId, setSelectedNodeId] = useState<string | null>(null);
  const [selectedEdgeId, setSelectedEdgeId] = useState<string | null>(null);
  const { screenToFlowPosition } = useReactFlow();

  const handleCancelExecution = async () => {
    if (!executionId) return;
    try {


      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const cancelReq: any = {
        params: { path: { executionId } },
        body: { reason: "User requested cancellation via UI" }
      };
      await apiClient.POST('/v1/executions/{executionId}/cancel', cancelReq);
      // The poll interval will pick up the cancelled state on next tick
    } catch (err) {
      console.error('Cancel error', err);
    }
  };


  const handleSelectExecution = async (execId: string) => {
    resetExecution();
    setExecutionId(execId);

    // Fetch execution to populate editor and node states
    try {
      const res = await apiClient.GET('/v1/executions/{executionId}', {
        params: { path: { executionId: execId } }
      });
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const dataAny = res.data as any;
      if (dataAny && dataAny.nodeStates) {
        const statuses: Record<string, string> = {};
        // eslint-disable-next-line @typescript-eslint/no-explicit-any
        Object.values(dataAny.nodeStates).forEach((state: any) => {
          if (state.nodeId && state.status) {
            statuses[state.nodeId] = state.status;
          }
        });
        setNodeStatuses(statuses);
      }
      // If we don't have the graph data for this execution, ideally we'd fetch it,
      // but GET /v1/executions/{executionId} doesn't return graphDefinition in the proto schema right now.
      // So we just rely on what is in the editor, or clear it if it doesn't match.
      // We will fetch anyway and see if there is metadata.
      if (dataAny?.status === 'EXECUTION_STATUS_RUNNING' || dataAny?.status === 'EXECUTION_STATUS_PENDING') {
         setIsRunning(true);
         startPolling(execId);
      } else {
         setIsRunning(false);
      }
    } catch (err) {
      console.error(err);
    }
  };

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

      // Use any for node config to avoid complex typing for the parser
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      type NodeDef = { id: string; type?: string; name?: string; [key: string]: any };
      type EdgeDef = { from: string; to: string; condition?: string };

      const nodeDefs = parsedObj.nodes as NodeDef[];

      // Client-side Validation: Unique IDs
      const nodeIds = new Set<string>();
      for (const n of nodeDefs) {
        if (!n.id) throw new Error('All nodes must have an "id"');
        if (nodeIds.has(n.id)) {
          throw new Error(`Duplicate node ID found: ${n.id}`);
        }
        nodeIds.add(n.id);
      }

      const reactFlowNodes: ReactFlowNode[] = nodeDefs.map((node) => ({
        id: node.id,
        type: 'custom',
        position: { x: 0, y: 0 }, // Dagre will position
        data: {
          id: node.id,
          type: node.type || 'unknown',
          label: node.name || node.id,
          status: nodeStatuses[node.id] || 'NODE_STATUS_UNSPECIFIED',
          config: node,
        },
      }));

      const edgesArr = Array.isArray(parsedObj.edges) ? (parsedObj.edges as EdgeDef[]) : [];

      // Client-side Validation: Dangling edges
      for (const e of edgesArr) {
        if (!nodeIds.has(e.from)) throw new Error(`Edge references unknown source node: ${e.from}`);
        if (!nodeIds.has(e.to)) throw new Error(`Edge references unknown target node: ${e.to}`);
      }

      // Client-side Validation: Cycles (simple DFS)
      const adjList = new Map<string, string[]>();
      for (const id of nodeIds) adjList.set(id, []);
      for (const e of edgesArr) adjList.get(e.from)?.push(e.to);

      const visited = new Set<string>();
      const recursionStack = new Set<string>();

      const detectCycle = (nodeId: string): boolean => {
        if (!visited.has(nodeId)) {
          visited.add(nodeId);
          recursionStack.add(nodeId);

          for (const neighbor of adjList.get(nodeId) || []) {
            if (!visited.has(neighbor) && detectCycle(neighbor)) return true;
            else if (recursionStack.has(neighbor)) return true;
          }
        }
        recursionStack.delete(nodeId);
        return false;
      };

      for (const id of nodeIds) {
        if (detectCycle(id)) {
          throw new Error(`Cycle detected involving node: ${id}`);
        }
      }

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

      const { layoutedNodes, layoutedEdges } = getLayoutedElements(reactFlowNodes, reactFlowEdges);

      return { nodes: layoutedNodes, edges: layoutedEdges, error: null };
    } catch (err: unknown) {
      if (err instanceof Error) {
        return { nodes: [], edges: [], error: `Parse/Validation Error: ${err.message}` };
      }
      return { nodes: [], edges: [], error: 'Parse/Validation Error: Unknown error' };
    }
  }, [inputData, nodeStatuses]);

  const displayError = fetchError || parsedGraphResult.error;

  // Sync internal UI state with textual editor logic
  const [nodes, setNodes] = useState<ReactFlowNode[]>(parsedGraphResult.nodes);
  const [edges, setEdges] = useState<Edge[]>(parsedGraphResult.edges);

  const [prevInputData, setPrevInputData] = useState(inputData);
  if (inputData !== prevInputData) {
    setPrevInputData(inputData);
    setNodes(parsedGraphResult.nodes);
    setEdges(parsedGraphResult.edges);
  }

  const serializeGraph = (currentNodes: ReactFlowNode[], currentEdges: Edge[]) => {
    const graphObj = {
      apiVersion: 'graph/v1',
      id: executionId || 'authoring_graph',
      nodes: currentNodes.map(n => ({
          ...(typeof n.data.config === 'object' && n.data.config !== null ? n.data.config : {}),
        id: n.id,
        type: n.data.type,
        name: n.data.label !== n.id ? n.data.label : undefined
      })),
      edges: currentEdges.map(e => ({
        from: e.source,
        to: e.target,
        condition: e.label || undefined
      }))
    };

    // Very simple check to decide if we should emit yaml or json
    if (inputData.trim().startsWith('{')) {
      setInputData(JSON.stringify(graphObj, null, 2));
    } else {
      setInputData(jsyaml.dump(graphObj));
    }
  };

  const onNodesChange = useCallback(
    (changes: NodeChange[]) => {
      setNodes((nds) => {
        const nextNodes = applyNodeChanges(changes, nds);
        const hasAddOrRemove = changes.some(c => c.type === 'add' || c.type === 'remove');
        if (hasAddOrRemove) {
          serializeGraph(nextNodes, edges);
        }
        return nextNodes;
      });
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [edges, inputData]
  );

  const onEdgesChange = useCallback(
    (changes: EdgeChange[]) => {
      setEdges((eds) => {
        const nextEdges = applyEdgeChanges(changes, eds);
        const hasAddOrRemove = changes.some(c => c.type === 'add' || c.type === 'remove');
        if (hasAddOrRemove) {
          serializeGraph(nodes, nextEdges);
        }
        return nextEdges;
      });
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [nodes, inputData]
  );

  const onConnect = useCallback(
    (params: Connection) => {
      setEdges((eds) => {
        const nextEdges = addEdge(params, eds);
        serializeGraph(nodes, nextEdges);
        return nextEdges;
      });
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [nodes, inputData]
  );

  const onDragOver = useCallback((event: React.DragEvent) => {
    event.preventDefault();
    event.dataTransfer.dropEffect = 'move';
  }, []);

  const onDrop = useCallback(
    (event: React.DragEvent) => {
      event.preventDefault();

      const type = event.dataTransfer.getData('application/reactflow');
      if (typeof type === 'undefined' || !type) {
        return;
      }

      const position = screenToFlowPosition({
        x: event.clientX,
        y: event.clientY,
      });

      const newNodeId = `node-${Date.now()}`;

      const newNode: ReactFlowNode = {
        id: newNodeId,
        type: 'custom',
        position,
        data: {
          id: newNodeId,
          type,
          label: newNodeId,
          status: 'NODE_STATUS_UNSPECIFIED',
          config: { id: newNodeId, type }
        },
      };

      setNodes((nds) => {
        const nextNodes = nds.concat(newNode);
        serializeGraph(nextNodes, edges);
        return nextNodes;
      });
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [screenToFlowPosition, edges, inputData]
  );

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
          onClick={() => {
            const blob = new Blob([inputData], { type: 'application/json' });
            const url = URL.createObjectURL(blob);
            const a = document.createElement('a');
            a.href = url;
            a.download = 'graph.json';
            a.click();
            URL.revokeObjectURL(url);
          }}
          disabled={!inputData.trim() || !!parsedGraphResult.error}
          style={{
            padding: '8px 16px',
            backgroundColor: (!inputData.trim() || !!parsedGraphResult.error) ? '#555' : '#2196f3',
            color: 'white',
            border: 'none',
            borderRadius: '4px',
            cursor: (!inputData.trim() || !!parsedGraphResult.error) ? 'not-allowed' : 'pointer',
            fontWeight: 'bold'
          }}
        >
          Export JSON
        </button>

        <button
          onClick={() => {
            const blob = new Blob([inputData], { type: 'text/yaml' });
            const url = URL.createObjectURL(blob);
            const a = document.createElement('a');
            a.href = url;
            a.download = 'graph.yaml';
            a.click();
            URL.revokeObjectURL(url);
          }}
          disabled={!inputData.trim() || !!parsedGraphResult.error}
          style={{
            padding: '8px 16px',
            backgroundColor: (!inputData.trim() || !!parsedGraphResult.error) ? '#555' : '#2196f3',
            color: 'white',
            border: 'none',
            borderRadius: '4px',
            cursor: (!inputData.trim() || !!parsedGraphResult.error) ? 'not-allowed' : 'pointer',
            fontWeight: 'bold'
          }}
        >
          Export YAML
        </button>

        <button
          id="run-btn"
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
        {isRunning && (
          <button
            onClick={handleCancelExecution}
            style={{
              padding: '8px 16px',
              backgroundColor: '#f44336',
              color: 'white',
              border: 'none',
              borderRadius: '4px',
              cursor: 'pointer',
              fontWeight: 'bold'
            }}
          >
            Cancel
          </button>
        )}
      </header>

      <div style={{ display: 'flex', flex: 1, overflow: 'hidden' }}>
        <ExecutionsSidebar onSelectExecution={handleSelectExecution} currentExecutionId={executionId} />
        <div style={{ flex: 1, position: 'relative', display: 'flex', flexDirection: 'column' }}>
          <div style={{ position: 'absolute', top: 0, left: 0, right: 0, bottom: 0, display: 'flex' }}>
            <div style={{ flex: 1, position: 'relative' }}>
              {nodes.length > 0 || !executionId ? ( // Show canvas even if empty to allow authoring
                <GraphCanvas
                  nodes={nodes}
                  edges={edges}
                  onNodeClick={(_, node) => {
                    setSelectedNodeId(node.id);
                    setSelectedEdgeId(null);
                  }}
                  onEdgeClick={(_, edge) => {
                    setSelectedEdgeId(edge.id);
                    setSelectedNodeId(null);
                  }}
                  onNodesChange={onNodesChange}
                  onEdgesChange={onEdgesChange}
                  onConnect={onConnect}
                  onDrop={onDrop}
                  onDragOver={onDragOver}
                />
              ) : (
                <div style={{ height: '100%', overflowY: 'auto' }}>
                  <ExperimentsGallery onLoad={(path: string, run: boolean) => {
                    handleLoadExample(path).then(() => {
                      if (run) {
                        setTimeout(() => {
                          document.getElementById('run-btn')?.click();
                        }, 50);
                      }
                    });
                  }} />
                </div>
              )}

              {selectedNodeId && (
                <NodeDrawer
                  key={selectedNodeId}
                  executionId={executionId}
                  nodeId={selectedNodeId}
                  config={nodes.find(n => n.id === selectedNodeId)?.data.config}
                  onClose={() => setSelectedNodeId(null)}
                  onConfigChange={(newConfig) => {
                    setNodes((nds) => {
                      const nextNodes = nds.map((node) => {
                        if (node.id === selectedNodeId) {
                          // Allow id change by effectively re-creating node object data
                          const finalId = newConfig.id || node.id;
                          return {
                            ...node,
                            id: finalId, // React flow node ID
                            data: {
                              ...node.data,
                              id: finalId,
                              type: newConfig.type || node.data.type,
                              label: newConfig.name || finalId,
                              config: newConfig
                            }
                          };
                        }
                        return node;
                      });
                      serializeGraph(nextNodes, edges);
                      return nextNodes;
                    });

                    // If ID changed, close drawer or update selected node id
                    if (newConfig.id && newConfig.id !== selectedNodeId) {
                       setSelectedNodeId(newConfig.id);
                    }
                  }}
                />
              )}

              {selectedEdgeId && (
                <EdgeDrawer
                  key={selectedEdgeId}
                  edgeId={selectedEdgeId}
                  condition={edges.find(e => e.id === selectedEdgeId)?.label as string | undefined}
                  onClose={() => setSelectedEdgeId(null)}
                  onConditionChange={(newCondition) => {
                    setEdges((eds) => {
                      const nextEdges = eds.map((edge) => {
                        if (edge.id === selectedEdgeId) {
                          return {
                            ...edge,
                            label: newCondition
                          };
                        }
                        return edge;
                      });
                      serializeGraph(nodes, nextEdges);
                      return nextEdges;
                    });
                  }}
                />
              )}
            </div>

            <NodePalette />
          </div>
        </div>

        <div style={{ width: '30%', minWidth: '300px', borderLeft: '1px solid #ccc', display: 'flex', flexDirection: 'column' }}>
          <div style={{ padding: '10px', background: '#eee', fontWeight: 'bold' }}>Input/Output (JSON/YAML)</div>
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
      </div>
    </div>
  );
}

function App() {
  return (
    <ReactFlowProvider>
      <AppFlow />
    </ReactFlowProvider>
  );
}

export default App;
