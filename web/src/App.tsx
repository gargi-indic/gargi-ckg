import { useEffect, useState } from 'react';
import './App.css';

function App() {
  const [storeHealth, setStoreHealth] = useState<string>('unknown');
  const [temporalHealth, setTemporalHealth] = useState<string>('unknown');

  useEffect(() => {
    const checkHealth = async () => {
      try {
        const baseUrl = import.meta.env.VITE_API_BASE_URL || 'http://localhost:8080';
        const response = await fetch(`${baseUrl}/readyz`);
        if (response.ok) {
          const data = await response.json();
          setStoreHealth(data.store === 'ok' ? 'ok' : 'error');
          setTemporalHealth(data.temporal === 'ok' ? 'ok' : 'error');
        } else {
          setStoreHealth('error');
          setTemporalHealth('error');
        }
      } catch {
        setStoreHealth('error');
        setTemporalHealth('error');
      }
    };

    checkHealth();
  }, []);

  return (
    <div className="App">
      <h1>Gargi Flow UI</h1>
      <div style={{ display: 'flex', gap: '1rem', justifyContent: 'center', marginTop: '2rem' }}>
        <div style={{ padding: '1rem', border: '1px solid #ccc', borderRadius: '4px' }}>
          <h3>Store Health</h3>
          <span style={{
            color: storeHealth === 'ok' ? 'green' : storeHealth === 'error' ? 'red' : 'gray',
            fontWeight: 'bold'
          }}>
            {storeHealth.toUpperCase()}
          </span>
        </div>
        <div style={{ padding: '1rem', border: '1px solid #ccc', borderRadius: '4px' }}>
          <h3>Temporal Health</h3>
          <span style={{
            color: temporalHealth === 'ok' ? 'green' : temporalHealth === 'error' ? 'red' : 'gray',
            fontWeight: 'bold'
          }}>
            {temporalHealth.toUpperCase()}
          </span>
        </div>
      </div>
    </div>
  );
}

export default App;
