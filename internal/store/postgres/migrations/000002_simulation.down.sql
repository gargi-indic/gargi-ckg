-- 000002_simulation.down.sql
-- Revert the simulation engine tables; the state store tables of 000001
-- are kept.

DROP TABLE IF EXISTS sim_reports CASCADE;
DROP TABLE IF EXISTS sim_experiments CASCADE;
DROP TABLE IF EXISTS sim_model_policies CASCADE;
DROP TABLE IF EXISTS sim_models CASCADE;
