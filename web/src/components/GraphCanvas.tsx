import React from 'react';
import {
  ReactFlow,
  MarkerType,
} from '@xyflow/react';
import type {
  Edge,
  Node as ReactFlowNode,
  Connection,
  EdgeChange,
  NodeChange,
} from '@xyflow/react';
import { useRef } from 'react';
import CustomNode from './CustomNode';
import '@xyflow/react/dist/style.css';

const nodeTypes = {
  custom: CustomNode,
};

interface GraphCanvasProps {
  nodes: ReactFlowNode[];
  edges: Edge[];
  onNodeClick?: (event: React.MouseEvent, node: ReactFlowNode) => void;
  onEdgeClick?: (event: React.MouseEvent, edge: Edge) => void;
  onNodesChange?: (changes: NodeChange[]) => void;
  onEdgesChange?: (changes: EdgeChange[]) => void;
  onConnect?: (connection: Connection) => void;
  onDrop?: (event: React.DragEvent) => void;
  onDragOver?: (event: React.DragEvent) => void;
}

const GraphCanvas: React.FC<GraphCanvasProps> = ({
  nodes,
  edges,
  onNodeClick,
  onEdgeClick,
  onNodesChange,
  onEdgesChange,
  onConnect,
  onDrop,
  onDragOver
}) => {
  const reactFlowWrapper = useRef<HTMLDivElement>(null);

  return (
    <div ref={reactFlowWrapper} style={{ width: '100%', height: '100%' }}>
      <ReactFlow
        nodes={nodes}
        edges={edges}
        onNodesChange={onNodesChange}
        onEdgesChange={onEdgesChange}
        onConnect={onConnect}
        onNodeClick={onNodeClick}
        onEdgeClick={onEdgeClick}
        onDrop={onDrop}
        onDragOver={onDragOver}
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
