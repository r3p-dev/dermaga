-- Migration 0006_m8_domains.sql
-- Domain DNS gate and certificate state (APP-2, TLS-2, TLS-3, DEC-38).
--   dns_verified_at:   when dermaga's DNS check first passed; NULL keeps the domain out of the route
--   cert_requested_at: when the domain last entered the route (Traefik requests its certificate then);
--                      a Traefik ACME error newer than this counts as a failure
--   cert_failed_at:    set when the order failed and the domain had no certificate; the domain leaves
--                      the route until "Check again" or the hourly retry clears it
--   cert_error:        Traefik's error text for that failure

ALTER TABLE domains ADD COLUMN dns_verified_at TEXT;
ALTER TABLE domains ADD COLUMN cert_requested_at TEXT;
ALTER TABLE domains ADD COLUMN cert_failed_at TEXT;
ALTER TABLE domains ADD COLUMN cert_error TEXT NOT NULL DEFAULT '';

-- Domains that exist before 0.8.0 are already routed: keep them routed across the upgrade.
UPDATE domains SET
    dns_verified_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now'),
    cert_requested_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now');
