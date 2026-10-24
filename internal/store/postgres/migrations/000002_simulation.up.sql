-- 000002_simulation.up.sql
-- Simulation engine persistence: model bundles, their policy documents,
-- experiment records and experiment reports (sim.ExperimentStore).
--
-- Documents and reports are BYTEA, not JSONB: they must round-trip byte for
-- byte (JSONB reorders keys and reformats numbers). IDs use the "C"
-- collation so ordering and keyset pagination compare bytes, like the
-- in-memory store.

CREATE TABLE IF NOT EXISTS sim_models (
    id TEXT COLLATE "C" NOT NULL,
    version INTEGER NOT NULL CHECK (version >= 1),
    format VARCHAR(16) NOT NULL,
    sim BYTEA NOT NULL,
    graph BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (id, version)
);

CREATE TABLE IF NOT EXISTS sim_model_policies (
    model_id TEXT COLLATE "C" NOT NULL,
    model_version INTEGER NOT NULL,
    name TEXT COLLATE "C" NOT NULL,
    document BYTEA NOT NULL,
    PRIMARY KEY (model_id, model_version, name),
    FOREIGN KEY (model_id, model_version) REFERENCES sim_models (id, version) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS sim_experiments (
    id TEXT COLLATE "C" PRIMARY KEY,
    model_id TEXT NOT NULL,
    model_version TEXT NOT NULL,
    definition BYTEA NOT NULL,
    format VARCHAR(16) NOT NULL,
    status VARCHAR(32) NOT NULL,
    error TEXT NOT NULL DEFAULT '',
    progress JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL,
    started_at TIMESTAMPTZ NULL,
    finished_at TIMESTAMPTZ NULL
);

CREATE INDEX IF NOT EXISTS idx_sim_experiments_created_id ON sim_experiments (created_at DESC, id DESC);

CREATE TABLE IF NOT EXISTS sim_reports (
    experiment_id TEXT COLLATE "C" PRIMARY KEY REFERENCES sim_experiments (id) ON DELETE CASCADE,
    report BYTEA NOT NULL
);
