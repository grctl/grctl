---
title: Server
---

The `grctld` binary is the Ground Control server daemon. It runs the workflow execution engine and, by default, an embedded NATS broker.

```bash
grctld [flags]
```

Use `grctld --help` or `grctld [command] --help` for detailed CLI information.

## Flags

These flags can be passed directly to `grctld`:

| Flag | Default | Description |
| :--- | :--- | :--- |
| `-c, --config string` | `config/grctl.yaml` | Path to the configuration file. |
| `-l, --log-level string` | `info` | Log level: `debug`, `info`, `warn`, or `error`. Can also be set via `LOG_LEVEL`. |
| `--port int` | — | Override the embedded NATS server port (embedded mode only). Takes precedence over the config file. |
| `--in-memory` | `false` | Use in-memory JetStream storage. **Data is lost on restart.** Takes precedence over the config file. |

## Configuration File

`grctld` loads configuration from a YAML file (default: `config/grctl.yaml`). If the file is not found, built-in defaults are used.

### Full Reference

```yaml
# NATS broker configuration
nats:
  # Mode of operation: "embedded" (default) or "external"
  mode: embedded

  # URL of the external NATS server (required when mode is "external")
  # url: "nats://127.0.0.1:4222"

  # Optional path to a custom NATS server config file (embedded mode only).
  # See: https://docs.nats.io/running-a-nats-service/configuration
  # config_file: "config/nats.conf"

  # Port the embedded NATS server listens on (embedded mode only).
  # Default: 4225
  port: 4225

# JetStream stream storage
streams:
  # Storage backend: "file" (default, persistent) or "memory" (lost on restart)
  storage: file

# Workflow execution defaults
defaults:
  # How long the server waits for a worker to acknowledge a task
  # Default: 5s
  worker_response_timeout: 5s

  # Maximum time a single workflow step is allowed to run
  # Default: 5m
  step_timeout: 5m
```

### Configuration Precedence

Configuration is applied in the following order (later sources take precedence):

1. **Built-in defaults**
2. **Config file** (`config/grctl.yaml`, or the path set with `-c`)
3. **Environment variables** (prefixed with `grctl_`)
4. **CLI flags** (e.g., `--port`)

## Environment Variables

All config file fields can be overridden using `grctl_`-prefixed environment variables. The mapping converts YAML path separators (`.`) to underscores (`_`).

| Environment Variable | Equivalent Config Key | Description |
| :--- | :--- | :--- |
| `LOG_LEVEL` | — | Log level override (also accepted by `-l`). |
| `grctl_NATS_PORT` | `nats.port` | Embedded NATS server port. |
| `grctl_NATS_MODE` | `nats.mode` | NATS mode (`embedded` or `external`). |
| `grctl_NATS_URL` | `nats.url` | URL of the external NATS server. |
| `grctl_NATS_CONFIG_FILE` | `nats.config_file` | Path to a custom NATS config file. |
| `grctl_STREAMS_STORAGE` | `streams.storage` | JetStream storage type (`file` or `memory`). |
| `grctl_DEFAULTS_WORKER_RESPONSE_TIMEOUT` | `defaults.worker_response_timeout` | Worker response timeout (e.g. `10s`). |
| `grctl_DEFAULTS_STEP_TIMEOUT` | `defaults.step_timeout` | Maximum step execution time (e.g. `10m`). |

## NATS Modes

### Embedded (default)

`grctld` starts its own NATS server process internally. No external NATS installation is required. This is the simplest way to get started.

```yaml
nats:
  mode: embedded
  port: 4225
```

You can optionally point `grctld` at a custom NATS configuration file for fine-grained broker tuning. See the [NATS server configuration reference](https://docs.nats.io/running-a-nats-service/configuration) for available options.

```yaml
nats:
  mode: embedded
  config_file: config/nats.conf
```

### External

Connect `grctld` to an existing NATS server. The `nats.url` field is required. The `port` field is ignored in this mode.

```yaml
nats:
  mode: external
  url: "nats://127.0.0.1:4222"
```

## Durability: JetStream `sync_interval`

By default, `grctld` configures JetStream with `sync_interval: always` (accepted values: `always`, or a duration like `10s`, `1m`). This forces an `fsync` after **every** write to the stream, guaranteeing that acknowledged data survives a power loss or kernel panic on a single-node deployment. This is the safest possible setting and the right default for a single-server install.

The trade-off is throughput: every write hits the disk synchronously, which limits write rate to what the underlying storage can fsync per second.

### When to relax it

If you are running a **3+ node NATS cluster**, JetStream replicates each message to a quorum of nodes before acknowledging the write. Quorum replication already provides durability — losing one node does not lose data — so per-write fsync becomes redundant. In that case, remove the override (or set it to a periodic interval like `10s`) to recover throughput.

To change it, point `grctld` at a custom NATS config and set `sync_interval` explicitly:

```conf
# config/nats.conf
jetstream {
    store_dir: "./data"
    sync_interval: 10s   # or remove to use the NATS default (2m)
}
```

```yaml
# config/grctl.yaml
nats:
  mode: embedded
  config_file: config/nats.conf
```

See the [NATS persistence docs](https://docs.nats.io/nats-concepts/jetstream#persistent-and-consistent-distributed-storage) for background.

## Shutdown

The server listens for `SIGINT` or `SIGTERM` and performs a graceful shutdown. In-progress work is allowed to complete within a **30-second timeout** before the process exits.

## Subcommands

### `grctld version`

Prints the version, commit hash, and build date of the `grctld` binary.

```
grctld dev
  commit: none
  built:  unknown
```
