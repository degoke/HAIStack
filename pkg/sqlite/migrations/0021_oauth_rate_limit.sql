CREATE TABLE IF NOT EXISTS hai_oauth_rate_limit (
    bucket_key     TEXT NOT NULL,
    endpoint       TEXT NOT NULL,
    window_start   TEXT NOT NULL,
    request_count  INTEGER NOT NULL,
    PRIMARY KEY (bucket_key, endpoint)
);
