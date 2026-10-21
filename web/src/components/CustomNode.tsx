import React from 'react';
import { Handle, Position } from '@xyflow/react';

interface CustomNodeProps {
  data: {
    id: string;
    type: string;
    label?: string;
    status?: string; // Optional status for the ring
  };
}

const CustomNode: React.FC<CustomNodeProps> = ({ data }) => {
  // Determine color based on status or default
  let ringColor = '#ccc';
  if (data.status === 'success') ringColor = '#4caf50';
  if (data.status === 'error') ringColor = '#f44336';
  if (data.status === 'running') ringColor = '#2196f3';
  if (data.status === 'pending') ringColor = '#ff9800';

  return (
    <div
      style={{
        padding: '10px',
        border: `3px solid ${ringColor}`,
        borderRadius: '8px',
        background: '#fff',
        minWidth: '150px',
        textAlign: 'center',
        boxShadow: '0 4px 6px rgba(0,0,0,0.1)',
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
      <Handle type="source" position={Position.Bottom} />
    </div>
  );
};

export default CustomNode;
