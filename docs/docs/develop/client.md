---
title: Client
---

The `Client` is how external code starts workflows and interacts with them. It connects to the server over NATS and returns handles you can use to send events or wait for results.

## Connecting

Create a `Connection` first, then pass it to the `Client`:

```python
from grctl.nats.connection import Connection
from grctl.client.client import Client

connection = await Connection.connect()
client = Client(connection=connection)
```

`Connection.connect()` reads the server address from the `GRCTL_NATS_SERVERS` environment variable. The default is `nats://localhost:4225`. You can also pass servers explicitly:

```python
connection = await Connection.connect(servers=["nats://prod-server:4225"])
```

`Connection` is a singleton — calling `connect()` a second time returns the same instance.

## Starting a Workflow and Waiting for the Result

Use `run_workflow()` to start a workflow and block until it completes:

```python
result = await client.run_workflow(
    type="ProcessOrder",
    id="order-abc-123",
    input={"order_id": "abc-123", "amount": 49.99},
    timeout=timedelta(minutes=5),
)
print(result)  # the value passed to ctx.next.complete(...)
```

`run_workflow()` raises if the workflow fails, times out, or is cancelled:

```python
from grctl.models.errors import WorkflowError

try:
    result = await client.run_workflow(...)
except WorkflowError as e:
    print(f"Workflow failed: {e}")
except TimeoutError:
    print("Workflow timed out")
except asyncio.CancelledError:
    print("Workflow was cancelled")
```

## Getting a Handle

Use `start_workflow()` when you need to send events to the workflow or don't want to block on the result:

```python
handle = await client.start_workflow(
    type="ProcessOrder",
    id="order-abc-123",
    input={"order_id": "abc-123", "amount": 49.99},
    timeout=timedelta(minutes=30),
)
```

The workflow starts immediately. `start_workflow()` returns as soon as the start command is sent — it does not wait for the workflow to finish.

## Sending Events

Use `handle.send()` to push events into a running workflow:

```python
await handle.send("approve")
await handle.send("update_quantity", payload={"item_id": "sku-1", "qty": 3})
```

See [Events](events.md) for how the workflow receives and processes them.

## Waiting for the Result

`handle.future` is an `asyncio.Future` that resolves when the workflow completes:

```python
# Wait indefinitely
result = await handle.future

# Wait with a timeout
result = await asyncio.wait_for(handle.future, timeout=60)
```

The future resolves to the value passed to `ctx.next.complete(result)` inside the workflow. It raises `WorkflowError`, `TimeoutError`, or `asyncio.CancelledError` on failure.

### Sending events then waiting

```python
handle = await client.start_workflow(
    type="GreetEvents",
    id="greet-session-1",
    input={"name": "Cem"},
    timeout=timedelta(seconds=30),
)

await handle.send("greet")
await handle.send("farewell")

result = await asyncio.wait_for(handle.future, timeout=30)
```

## Getting a Handle for an Existing Workflow

Use `get_handle()` to get a handle to a workflow that is already running. You only need the `workflow_id`:

```python
handle = await client.get_handle(workflow_id="order-abc-123")

# Send events, run queries, or wait for the result
await handle.send("approve")
result = await handle.future
```

This is useful when a different part of your system needs to interact with a workflow it didn't start. For example, a webhook handler that sends an approval event to a running order workflow.

See [Workflow ID and Runs](../workflows/#workflow-id-and-runs) for how workflow identity and deduplication work.

## Reference

### `Client`

| Method | Description |
|---|---|
| `Client(connection)` | Create a client using an existing `Connection`. |
| `await client.run_workflow(...)` | Start a workflow and wait for its result. |
| `await client.start_workflow(...)` | Start a workflow and return a handle. |
| `await client.get_handle(workflow_id)` | Get a handle to an existing workflow. |

### `run_workflow()` / `start_workflow()` Parameters

| Parameter | Type | Default | Description |
|---|---|---|---|
| `type` | `str` | — | The `workflow_type` string of the workflow to run. |
| `id` | `str` | — | Stable caller-supplied identifier. Used for deduplication. |
| `input` | `Any \| None` | `None` | Input passed to the workflow's start handler as keyword arguments. |
| `timeout` | `timedelta \| None` | `None` | Maximum duration before the workflow is failed with `TimeoutError`. |

### `WorkflowHandle`

| Member | Type | Description |
|---|---|---|
| `handle.future` | `asyncio.Future` | Resolves to the workflow result. Raises on failure. |
| `handle.run_info` | `RunInfo` | Metadata: `run_info.id` (run ID), `run_info.wf_id` (workflow ID), `run_info.wf_type`. |
| `await handle.send(event_name, payload)` | — | Send an event to the workflow. |

### `Connection.connect()`

| Parameter | Type | Default | Description |
|---|---|---|---|
| `servers` | `list[str] \| None` | `None` | NATS server URLs. Defaults to `grctl_NATS_SERVERS` env var or `nats://localhost:4225`. |
