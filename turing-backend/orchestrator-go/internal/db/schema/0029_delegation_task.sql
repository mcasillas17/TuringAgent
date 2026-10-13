-- A delegation records the task it was asked to do, as the model wrote it in
-- team.delegate's task argument, so a card rebuilt after a restart says what
-- the specialist is working on. Delegations made before this read empty.
ALTER TABLE delegations ADD COLUMN task TEXT NOT NULL DEFAULT '';
