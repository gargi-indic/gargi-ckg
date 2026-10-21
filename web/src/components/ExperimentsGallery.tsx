import React from 'react';

const EXPERIMENTS = [
  {
    id: 'linear_pipeline',
    title: 'Linear Pipeline',
    description: 'A simple sequential pipeline of 3 nodes: Format data, Enrich data, and Save data.',
    path: '/examples/linear_pipeline.json'
  },
  {
    id: 'fanout_fanin',
    title: 'Fan-out / Fan-in',
    description: 'Executes multiple processing tasks in parallel and waits for all to complete before reducing.',
    path: '/examples/fanout_fanin.json'
  },
  {
    id: 'conditional_branching',
    title: 'Conditional Branching',
    description: 'Uses CEL expressions to evaluate inputs and route execution down different paths.',
    path: '/examples/conditional_branch.json'
  }
];

interface Props {
  onLoad: (path: string, runImmediately: boolean) => void;
}

export const ExperimentsGallery: React.FC<Props> = ({ onLoad }) => {
  return (
    <div style={{ padding: '20px', maxWidth: '800px', margin: '0 auto' }}>
      <h2>Experiments Gallery</h2>
      <p style={{ color: '#666', marginBottom: '20px' }}>Load and run example graph definitions to see the engine in action.</p>

      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(250px, 1fr))', gap: '20px' }}>
        {EXPERIMENTS.map(exp => (
          <div key={exp.id} style={{
            border: '1px solid #ddd',
            borderRadius: '8px',
            padding: '16px',
            background: 'white',
            boxShadow: '0 2px 4px rgba(0,0,0,0.05)'
          }}>
            <h3 style={{ marginTop: 0, marginBottom: '8px', color: '#333' }}>{exp.title}</h3>
            <p style={{ fontSize: '13px', color: '#666', marginBottom: '16px', minHeight: '60px' }}>
              {exp.description}
            </p>
            <div style={{ display: 'flex', gap: '10px' }}>
              <button
                onClick={() => onLoad(exp.path, false)}
                style={{ flex: 1, padding: '6px', background: '#f5f5f5', border: '1px solid #ccc', borderRadius: '4px', cursor: 'pointer' }}
              >
                Load Code
              </button>
              <button
                onClick={() => onLoad(exp.path, true)}
                style={{ flex: 1, padding: '6px', background: '#2196f3', color: 'white', border: 'none', borderRadius: '4px', cursor: 'pointer' }}
              >
                Load & Run
              </button>
            </div>
          </div>
        ))}
      </div>
    </div>
  );
};
