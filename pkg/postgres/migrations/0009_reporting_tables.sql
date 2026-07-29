-- Analytics reporting tables for bytefhir-analytics edge mode.

CREATE TABLE IF NOT EXISTS hai_analytics_reporting_meta (
    tenant_id     TEXT NOT NULL REFERENCES hai_tenant (id),
    view_name     TEXT NOT NULL,
    view_version  TEXT NOT NULL,
    columns       JSONB NOT NULL,
    row_count     INT NOT NULL DEFAULT 0,
    refreshed_at  TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (tenant_id, view_name, view_version)
);

CREATE TABLE IF NOT EXISTS hai_analytics_reporting_row (
    tenant_id     TEXT NOT NULL REFERENCES hai_tenant (id),
    view_name     TEXT NOT NULL,
    view_version  TEXT NOT NULL,
    row_num       BIGINT NOT NULL,
    data          JSONB NOT NULL,
    PRIMARY KEY (tenant_id, view_name, view_version, row_num)
);

CREATE INDEX IF NOT EXISTS idx_analytics_reporting_row_lookup
    ON hai_analytics_reporting_row (tenant_id, view_name, view_version);
