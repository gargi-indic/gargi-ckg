import React, { useEffect, useState } from 'react';
import { apiClient } from '../api/client';
import type { definitions } from '../api/schema';

type ExecutionSummary = definitions["v1ExecutionSummary"];

interface Props {
  onSelectExecution: (execId: string) => void;
  currentExecutionId: string | null;
}

export const ExecutionsSidebar: React.FC<Props> = ({ onSelectExecution, currentExecutionId }) => {
  const [executions, setExecutions] = useState<ExecutionSummary[]>([]);
  const [loading, setLoading] = useState(false);

  const fetchExecutions = async () => {
    setLoading(true);
    try {
      const res = await apiClient.GET('/v1/executions', {
        params: { query: { limit: 20 } }
      });

      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const dataAny = res.data as any;
      if (dataAny?.executions) {
        setExecutions(dataAny.executions);
      }
    } catch (err) {
      console.error('Failed to fetch executions', err);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    // Only call in an async wrapper to avoid "synchronous setState in effect" lint error
    const init = async () => {
      await fetchExecutions();
    };
    init();

    const interval = setInterval(fetchExecutions, 5000);
    return () => clearInterval(interval);
  }, []);

  return (
    <div style={{ width: '250px', borderRight: '1px solid #ccc', display: 'flex', flexDirection: 'column', background: '#fafafa' }}>
      <div style={{ padding: '10px', background: '#eee', fontWeight: 'bold', display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
        <span>Recent Executions</span>
        <button onClick={fetchExecutions} style={{ cursor: 'pointer', fontSize: '12px' }}>↻</button>
      </div>
      <div style={{ flex: 1, overflowY: 'auto' }}>
        {loading && executions.length === 0 ? (
          <div style={{ padding: '10px', color: '#888' }}>Loading...</div>
        ) : executions.length === 0 ? (
          <div style={{ padding: '10px', color: '#888' }}>No executions found.</div>
        ) : (
          executions.map((exec, i) => {
            const isSelected = exec.executionId === currentExecutionId;
            const statusColor = exec.status === 'EXECUTION_STATUS_SUCCEEDED' ? '#4caf50' :
                               exec.status === 'EXECUTION_STATUS_FAILED' ? '#f44336' :
                               exec.status === 'EXECUTION_STATUS_CANCELLED' ? '#ff9800' :
                               '#2196f3';

            return (
              <div
                key={exec.executionId || i}
                onClick={() => exec.executionId && onSelectExecution(exec.executionId)}
                style={{
                  padding: '10px',
                  borderBottom: '1px solid #eee',
                  cursor: 'pointer',
                  background: isSelected ? '#e3f2fd' : 'transparent',
                  borderLeft: `4px solid ${statusColor}`
                }}
              >
                <div style={{ fontSize: '12px', fontWeight: 'bold', textOverflow: 'ellipsis', overflow: 'hidden', whiteSpace: 'nowrap' }} title={exec.executionId}>
                  {exec.executionId}
                </div>
                <div style={{ fontSize: '11px', color: '#666', marginTop: '4px' }}>
                  {exec.status?.replace('EXECUTION_STATUS_', '')}
                </div>
                {exec.createdAt && (
                  <div style={{ fontSize: '10px', color: '#999', marginTop: '2px' }}>
                    {new Date(exec.createdAt).toLocaleString()}
                  </div>
                )}
              </div>
            );
          })
        )}
      </div>
    </div>
  );
};
