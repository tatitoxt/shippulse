-- ShipPulse ClickHouse Telemetry Database Initialization

CREATE DATABASE IF NOT EXISTS shippulse_telemetry;

USE shippulse_telemetry;

-- 1. Logs Table
CREATE TABLE IF NOT EXISTS telemetry_logs (
    timestamp DateTime64(3, 'UTC') DEFAULT now64(3),
    organization_id String,
    service_id String,
    environment_id String,
    severity String, -- INFO, WARN, ERROR, DEBUG
    trace_id String,
    span_id String,
    message String,
    attributes Map(String, String)
) ENGINE = MergeTree()
PARTITION BY toYYYYMM(timestamp)
ORDER BY (organization_id, service_id, timestamp)
TTL timestamp + INTERVAL 14 DAY;

-- 2. Metrics Table
CREATE TABLE IF NOT EXISTS telemetry_metrics (
    timestamp DateTime64(3, 'UTC') DEFAULT now64(3),
    organization_id String,
    service_id String,
    environment_id String,
    metric_name String, -- e.g. http.request.duration_ms, http.requests.total, cpu.utilization
    value Float64,
    attributes Map(String, String)
) ENGINE = MergeTree()
PARTITION BY toYYYYMM(timestamp)
ORDER BY (organization_id, metric_name, service_id, timestamp)
TTL timestamp + INTERVAL 30 DAY;

-- 3. Traces & Spans Table
CREATE TABLE IF NOT EXISTS telemetry_traces (
    timestamp DateTime64(3, 'UTC') DEFAULT now64(3),
    organization_id String,
    service_id String,
    trace_id String,
    span_id String,
    parent_span_id String,
    span_name String,
    duration_ms Float64,
    status_code String, -- OK, ERROR, UNSET
    attributes Map(String, String)
) ENGINE = MergeTree()
PARTITION BY toYYYYMM(timestamp)
ORDER BY (organization_id, trace_id, service_id, timestamp)
TTL timestamp + INTERVAL 14 DAY;
