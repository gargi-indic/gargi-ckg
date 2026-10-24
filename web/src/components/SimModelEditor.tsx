import { useState } from 'react';
import { validateModel } from '../sim/validate';
import type { SimDocs, ValidationIssue } from '../sim/validate';

type DocName = keyof SimDocs;

const TABS: { name: DocName; label: string }[] = [
  { name: 'graph', label: 'graph.yaml' },
  { name: 'sim', label: 'sim.yaml' },
  { name: 'experiment', label: 'experiment.yaml' },
];

type Props = {
  docs: SimDocs;
  onChange: (docs: SimDocs) => void;
};

export default function SimModelEditor({ docs, onChange }: Props) {
  const [active, setActive] = useState<DocName>('sim');
  const [issues, setIssues] = useState<ValidationIssue[] | null>(null);
  const [failure, setFailure] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const handleValidate = async () => {
    setBusy(true);
    setFailure(null);
    try {
      setIssues(await validateModel(docs));
    } catch (err) {
      setIssues(null);
      setFailure(`Validation failed: ${err instanceof Error ? err.message : 'unknown error'}`);
    } finally {
      setBusy(false);
    }
  };

  return (
    <div style={{ flex: 1, display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>
      <div style={{ display: 'flex', gap: '4px', padding: '8px', background: '#eee', alignItems: 'center' }}>
        <strong style={{ marginRight: '12px' }}>Simulation model</strong>
        {TABS.map((t) => (
          <button
            key={t.name}
            data-testid={`sim-tab-${t.name}`}
            onClick={() => setActive(t.name)}
            style={{
              padding: '6px 12px',
              border: '1px solid #ccc',
              borderRadius: '4px',
              cursor: 'pointer',
              background: active === t.name ? '#2196f3' : 'white',
              color: active === t.name ? 'white' : 'black',
            }}
          >
            {t.label}
          </button>
        ))}
        <button
          data-testid="sim-validate"
          onClick={handleValidate}
          disabled={busy}
          style={{
            marginLeft: 'auto',
            padding: '6px 16px',
            border: 'none',
            borderRadius: '4px',
            cursor: busy ? 'not-allowed' : 'pointer',
            background: busy ? '#ccc' : '#4caf50',
            color: 'white',
            fontWeight: 'bold',
          }}
        >
          {busy ? 'Validating...' : 'Validate'}
        </button>
      </div>
      <textarea
        key={active}
        data-testid={`sim-editor-${active}`}
        value={docs[active]}
        onChange={(e) => onChange({ ...docs, [active]: e.target.value })}
        spellCheck={false}
        style={{ flex: 1, padding: '10px', boxSizing: 'border-box', fontFamily: 'monospace', resize: 'none' }}
      />
      {failure && (
        <div data-testid="sim-validate-failure" style={{ padding: '10px', color: 'red', background: '#ffebee', borderTop: '1px solid #ffcdd2' }}>
          {failure}
        </div>
      )}
      {issues && issues.length === 0 && (
        <div data-testid="sim-validate-ok" style={{ padding: '10px', color: '#1b5e20', background: '#e8f5e9', borderTop: '1px solid #c8e6c9' }}>
          No errors
        </div>
      )}
      {issues && issues.length > 0 && (
        <ul data-testid="sim-validate-errors" style={{ margin: 0, padding: '10px 10px 10px 30px', color: '#b71c1c', background: '#ffebee', borderTop: '1px solid #ffcdd2', maxHeight: '30%', overflowY: 'auto' }}>
          {issues.map((e, i) => (
            <li key={i}>
              {e.path} · {e.code} · {e.message}
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
