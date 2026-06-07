---
title: Worker
---

A worker connects to the server, pulls workflow step assignments from NATS, executes them in-process, and reports the results back. Workers are stateless. Any worker can handle any step for any registered workflow type.

## Basic Setup

```python
import asyncio
from grctl.nats.connection import Connection
from grctl.worker.worker import Worker
from grctl.workflow import Workflow

order_wf = Workflow(workflow_type="ProcessOrder")
# ... register handlers ...

async def main() -> None:
    connection = await Connection.connect()
    worker = Worker(
        workflows=[order_wf],
        connection=connection,
    )
    await worker.start()

asyncio.run(main())
```

`worker.start()` is a blocking call. It runs until the worker is stopped. Register all workflows your process handles in the `workflows` list.

## Multiple Workflow Types

A single worker can handle multiple workflow types. Pass all of them to the constructor:

```python
worker = Worker(
    workflows=[order_wf, payment_wf, notification_wf],
    connection=connection,
)
await worker.start()
```

The worker subscribes to step assignments for each registered type.

## Graceful Shutdown

Call `worker.stop()` to shut down cleanly. It stops accepting new work, waits for in-flight steps to finish, then closes the NATS connection:

```python
async def main() -> None:
    connection = await Connection.connect()
    worker = Worker(workflows=[order_wf], connection=connection)

    worker_task = asyncio.create_task(worker.start())

    try:
        await asyncio.sleep(3600)  # run for an hour
    finally:
        await worker.stop()
        worker_task.cancel()
        await asyncio.gather(worker_task, return_exceptions=True)
```

`stop()` accepts a `shutdown_timeout` (default: 30 seconds). If in-flight steps don't complete within that window, the worker terminates them and exits anyway.

### Handling OS signals

For long-running processes, wire `stop()` to `SIGINT` / `SIGTERM`:

```python
import asyncio
import signal
from grctl.nats.connection import Connection
from grctl.worker.worker import Worker

async def main() -> None:
    connection = await Connection.connect()
    worker = Worker(workflows=[order_wf], connection=connection)

    loop = asyncio.get_running_loop()
    for sig in (signal.SIGINT, signal.SIGTERM):
        loop.add_signal_handler(sig, lambda: asyncio.create_task(worker.stop()))

    await worker.start()

asyncio.run(main())
```

## Custom Logger

By default, `ctx.logger` inside handlers writes to the root Python logger. Pass a custom `logging.Logger` instance to route workflow logs where you want them:

```python
import logging

workflow_logger = logging.getLogger("my_app.workflows")
workflow_logger.setLevel(logging.DEBUG)

worker = Worker(
    workflows=[order_wf],
    connection=connection,
    workflow_logger=workflow_logger,
)
```

## Horizontal Scaling

Add more worker processes to increase throughput. Workers use NATS queue groups internally. The server distributes step assignments across all available workers automatically. No coordination is needed.

```
Process 1: Worker(workflows=[order_wf, payment_wf])
Process 2: Worker(workflows=[order_wf, payment_wf])
Process 3: Worker(workflows=[order_wf, payment_wf])
```

Each process handles a share of the work. If a worker crashes mid-step, the server's step timeout fires and the step is reassigned to another worker, which re-executes the step from the beginning (tasks that already completed are skipped — see [Tasks](tasks.md)).

There is no sticky routing. Any worker that has the workflow type registered can handle any step.

## Connecting to a Remote Server

By default, the worker connects to `nats://localhost:4225`. Override this with the `grctl_NATS_SERVERS` environment variable or by passing `servers` explicitly to `Connection.connect()`:

```python
connection = await Connection.connect(servers=["nats://prod-server:4225"])
```

## Reference

### `Worker`

| Parameter | Type | Default | Description |
|---|---|---|---|
| `workflows` | `list[Workflow]` | — | Workflow instances to handle. |
| `connection` | `Connection` | — | Active NATS connection. |
| `workflow_logger` | `logging.Logger | None` | `None` | Logger used by `ctx.logger` inside handlers. Defaults to the root logger. |

### Methods

| Method | Description |
|---|---|
| `await worker.start()` | Start processing. Blocks until stopped. |
| `await worker.stop(shutdown_timeout)` | Graceful shutdown. Waits up to `shutdown_timeout` seconds (default: 30) for in-flight steps to finish. |

### Worker Identity

| Property | Description |
|---|---|
| `worker.worker_id` | Unique per-process ID. Format: `<name>.<random>@<hostname>`. |
| `worker.worker_name` | Stable ID derived from the registered workflow types. Identical across processes with the same workflow set. |
