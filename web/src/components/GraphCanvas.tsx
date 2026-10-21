import React, { useEffect } from 'react';
import {
  ReactFlow,
  useNodesState,
  useEdgesState,
  MarkerType,
} from '@xyflow/react';
import type { Edge, Node as ReactFlowNode } from '@xyflow/react';
import dagre from 'dagre';
import CustomNode from './CustomNode';
import '@xyflow/react/dist/style.css';

const nodeTypes = {
  custom: CustomNode,
};

interface GraphCanvasProps {
  nodes: ReactFlowNode[];
  edges: Edge[];
}

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

const GraphCanvas: React.FC<GraphCanvasProps> = ({ nodes: initialNodes, edges: initialEdges }) => {
  const [nodes, setNodes, onNodesChange] = useNodesState(initialNodes);
  const [edges, setEdges, onEdgesChange] = useEdgesState(initialEdges);

  useEffect(() => {
    const { layoutedNodes, layoutedEdges } = getLayoutedElements(
      initialNodes,
      initialEdges,
      'TB'
    );

    setNodes([...layoutedNodes]);
    setEdges([...layoutedEdges]);
  }, [initialNodes, initialEdges, setNodes, setEdges]);

  return (
    <div style={{ width: '100%', height: '100%' }}>
      <ReactFlow
        nodes={nodes}
        edges={edges}
        onNodesChange={onNodesChange}
        onEdgesChange={onEdgesChange}
        nodeTypes={nodeTypes}
        fitView
        defaultEdgeOptions={{
          markerEnd: {
            type: MarkerType.ArrowClosed,
          },
        }}
      />
    </div>
  );
};

export default GraphCanvas;
