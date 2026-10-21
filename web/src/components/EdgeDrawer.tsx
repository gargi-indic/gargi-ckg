import React, { useState } from 'react';

interface EdgeDrawerProps {
  edgeId: string | null;
  condition: string | undefined;
  onClose: () => void;
  onConditionChange?: (newCondition: string | undefined) => void;
}

const EdgeDrawer: React.FC<EdgeDrawerProps> = ({ edgeId, condition, onClose, onConditionChange }) => {
  const [editingCondition, setEditingCondition] = useState<string>(condition || '');

  const [prevCondition, setPrevCondition] = useState(condition);
  if (condition !== prevCondition) {
    setPrevCondition(condition);
    setEditingCondition(condition || '');
  }

  if (!edgeId) return null;

  const handleSave = () => {
    if (onConditionChange) {
      onConditionChange(editingCondition.trim() === '' ? undefined : editingCondition);
    }
  };

  return (
    <div
      style={{
        position: 'absolute',
        top: 0,
        right: 0,
        width: '400px',
        height: '100%',
        backgroundColor: '#fff',
        boxShadow: '-4px 0 15px rgba(0,0,0,0.1)',
        display: 'flex',
        flexDirection: 'column',
        zIndex: 1000,
      }}
    >
      <div style={{ padding: '16px', borderBottom: '1px solid #ccc', display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
        <h2 style={{ margin: 0, fontSize: '1.2rem' }}>Edge: {edgeId}</h2>
        <button onClick={onClose} style={{ background: 'none', border: 'none', fontSize: '1.2rem', cursor: 'pointer' }}>✖</button>
      </div>

      <div style={{ flex: 1, overflowY: 'auto', padding: '16px' }}>
        <div style={{ display: 'flex', flexDirection: 'column', gap: '16px' }}>
          <div>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '4px' }}>
              <strong style={{ display: 'block' }}>Condition (CEL Expression)</strong>
              <button
                onClick={handleSave}
                style={{ fontSize: '0.8rem', padding: '2px 6px', cursor: 'pointer', backgroundColor: '#4caf50', color: 'white', border: 'none', borderRadius: '4px' }}
              >
                Save
              </button>
            </div>
            <textarea
              value={editingCondition}
              onChange={(e) => setEditingCondition(e.target.value)}
              placeholder="e.g. state.counter > 10"
              style={{ width: '100%', minHeight: '150px', padding: '8px', backgroundColor: '#f5f5f5', borderRadius: '4px', fontSize: '0.85rem', border: '1px solid #ddd', fontFamily: 'monospace', boxSizing: 'border-box' }}
            />
          </div>
        </div>
      </div>
    </div>
  );
};

export default EdgeDrawer;
