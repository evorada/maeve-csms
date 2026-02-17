-- Add last_cost column to transactions table to track running cost updates
-- communicated via OCPP 2.0.1 CostUpdated messages.
ALTER TABLE transactions ADD COLUMN IF NOT EXISTS last_cost NUMERIC(12, 4);
