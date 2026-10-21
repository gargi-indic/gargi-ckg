import React from 'react';
import { Handle, Position } from '@xyflow/react';

interface CustomNodeProps {
  data: {
    id: string;
    type: string;
    label?: string;
    status?: string;
  };
}

const CustomNode: React.FC<CustomNodeProps> = ({ data }) => {
  let ringColor = '#ccc';
  let animation = 'none';
  let opacity = 1;
  let borderStyle = 'solid';

  switch (data.status) {
    case 'NODE_STATUS_PENDING':
    case 'NODE_STATUS_READY':
      ringColor = '#9e9e9e'; // Grey
      break;
    case 'NODE_STATUS_RUNNING':
    case 'NODE_STATUS_COMPENSATING':
      ringColor = '#2196f3'; // Blue
      animation = 'pulse 1.5s infinite';
      break;
    case 'NODE_STATUS_SUCCEEDED':
    case 'NODE_STATUS_COMPENSATED':
      ringColor = '#4caf50'; // Green
      break;
    case 'NODE_STATUS_FAILED':
      ringColor = '#f44336'; // Red
      break;
    case 'NODE_STATUS_SKIPPED':
      ringColor = '#9e9e9e'; // Grey
      opacity = 0.5;
      borderStyle = 'dashed';
      break;
    default:
      ringColor = '#ccc'; // Default grey
  }

  const isAwaitingSignal = data.type === 'wait_for_signal' && data.status === 'NODE_STATUS_RUNNING';
  if (isAwaitingSignal) {
    ringColor = '#ff9800'; // Amber
  }

  return (
    <div
      className="custom-node"
      style={{
        padding: '10px',
        border: `3px ${borderStyle} ${ringColor}`,
        borderRadius: '8px',
        background: '#fff',
        minWidth: '150px',
        textAlign: 'center',
        boxShadow: '0 4px 6px rgba(0,0,0,0.1)',
        opacity: opacity,
        animation: animation,
      }}
    >
      <Handle type="target" position={Position.Top} />
      <div style={{ fontWeight: 'bold', fontSize: '14px', marginBottom: '4px' }}>
        {data.id}
      </div>
      <div style={{ fontSize: '12px', color: '#666' }}>
        Type: {data.type}
      </div>
      {data.label && data.label !== data.id && (
        <div style={{ fontSize: '12px', color: '#888', marginTop: '4px' }}>
          {data.label}
        </div>
      )}
      <div style={{ fontSize: '10px', color: ringColor, marginTop: '4px', fontWeight: 'bold' }}>
        {data.status ? data.status.replace('NODE_STATUS_', '') : 'UNSPECIFIED'}
      </div>
      {isAwaitingSignal && (
        <div
          data-testid="awaiting-signal-badge"
          style={{
            marginTop: '6px',
            padding: '2px 8px',
            display: 'inline-block',
            fontSize: '11px',
            fontWeight: 'bold',
            color: '#e65100',
            backgroundColor: '#fff3e0',
            border: '1px solid #ffcc80',
            borderRadius: '12px',
          }}
        >
          ⏳ Awaiting signal
        </div>
      )}
      <Handle type="source" position={Position.Bottom} />
    </div>
  );
};

export default CustomNode;
