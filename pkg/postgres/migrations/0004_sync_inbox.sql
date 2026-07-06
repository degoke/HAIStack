-- sync inbox idempotency for hub push dedupe and future pull apply on postgres nodes

CREATE TABLE IF NOT EXISTS sync_inbox_applied (
    tenant_id   TEXT NOT NULL REFERENCES tenant (id),
    id          TEXT NOT NULL,
    applied_at  TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (tenant_id, id)
);
