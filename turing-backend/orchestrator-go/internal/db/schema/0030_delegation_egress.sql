-- Child egress consent. A delegation whose specialist may use tools that leave
-- the machine (connected-account integrations, remote MCP servers) records
-- them, and the user's consent for this one task: pending until they decide,
-- then granted or declined. Its child run cannot be claimed while consent is
-- pending. egress_required is set when the specialist's `requires` is met only
-- by those tools, so declining ends the task instead of running it without
-- them. Delegations made before this asked for no such tools.
ALTER TABLE delegations ADD COLUMN egress_tools_json TEXT NOT NULL DEFAULT '[]';
ALTER TABLE delegations ADD COLUMN egress_consent TEXT NOT NULL DEFAULT ''
  CHECK (egress_consent IN ('', 'pending', 'granted', 'declined'));
ALTER TABLE delegations ADD COLUMN egress_required INTEGER NOT NULL DEFAULT 0 CHECK (egress_required IN (0, 1));
