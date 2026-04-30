---
title: Events
---

Events let external systems push signals into a running workflow. A workflow pauses at a `wait_for_event` transition and resumes when an event arrives. You can use events to implement approval flows, human-in-the-loop steps, webhooks, or any pattern where a workflow needs to react to something outside its own execution.

## Basic Usage

Define event handlers with `@workflow.event()`, then transition to a waiting state with `ctx.next.wait_for_event()`:

```python
from grctl.workflow import Workflow
from grctl.worker.context import Context
from grctl.models import Directive

order = Workflow(workflow_type="ProcessOrder")

@order.start()
async def start(ctx: Context, order_id: str) -> Directive:
    ctx.store.put("order_id", order_id)
    # Pause until an event arrives
    return ctx.next.wait_for_event()

@order.event()
async def approve(ctx: Context) -> Directive:
    ctx.store.put("status", "approved")
    return ctx.next.step(fulfill)

@order.event()
async def reject(ctx: Context) -> Directive:
    ctx.store.put("status", "rejected")
    return ctx.next.complete("rejected")
```

From the client, send an event to the running workflow using its handle:

```python
handle = await client.start_workflow(
    type="ProcessOrder",
    id="order-abc",
    input={"order_id": "abc"},
)

# Later, send an event
await handle.send("approve")
```

## Defining Event Handlers

Use `@workflow.event()` to register a handler. The function name becomes the event name by default. Use the `name` parameter to override it:

```python
@order.event()
async def approve(ctx: Context) -> Directive:
    # Triggered by handle.send("approve")
    ...

@order.event(name="order.rejected")
async def handle_rejection(ctx: Context) -> Directive:
    # Triggered by handle.send("order.rejected")
    ...
```

Event handlers receive `ctx` as their first argument and return a `Directive`, exactly like step handlers. They can read and write `ctx.store`, call tasks, and return any transition.

### Event Payload

Pass data with an event by providing a `payload` argument to `handle.send()`:

```python
await handle.send("update_quantity", payload={"item_id": "sku-1", "qty": 3})
```

If the payload is a dict, its keys are unpacked as keyword arguments:

```python
@order.event()
async def update_quantity(ctx: Context, item_id: str, qty: int) -> Directive:
    ctx.store.put(f"qty_{item_id}", qty)
    return ctx.next.wait_for_event()
```

If the payload is a scalar value (string, int, etc.), it is passed as a single positional argument:

```python
await handle.send("set_priority", payload="high")

@order.event()
async def set_priority(ctx: Context, priority: str) -> Directive:
    ctx.store.put("priority", priority)
    return ctx.next.wait_for_event()
```

## How Event Dispatch Works

Every incoming event is stored in the workflow's inbox regardless of the workflow's current state. The inbox preserves arrival order.

When the workflow returns `ctx.next.wait_for_event()`:
- If the inbox already has a waiting event, that event is dispatched immediately as a step without waiting.
- If the inbox is empty, the workflow enters `WaitEvent` state and stays there until an event arrives.

When an event arrives while the workflow is running a step (not yet waiting):
- The event is stored in the inbox.
- The workflow continues its current step to completion.
- On the next `ctx.next.wait_for_event()`, the queued event is dispatched.

This means events are never dropped, even if they arrive before the workflow is ready to receive them.

## Processing Multiple Events

A workflow can wait for many events over its lifetime. Return `ctx.next.wait_for_event()` from an event handler to pause and wait for the next one:

```python
greet = Workflow(workflow_type="GreetEvents")

@greet.start()
async def start(ctx: Context, name: str) -> Directive:
    ctx.store.put("name", name)
    return ctx.next.wait_for_event()

@greet.event()
async def greet(ctx: Context) -> Directive:
    name = await ctx.store.get("name")
    greeting = await call_greeting_api(name)
    ctx.store.put("message", greeting)
    return ctx.next.wait_for_event()  # wait for the next event

@greet.event()
async def farewell(ctx: Context) -> Directive:
    name = await ctx.store.get("name")
    result = await call_farewell_api(name)
    return ctx.next.complete(result)
```

The client drives the workflow by sending events in sequence:

```python
await handle.send("greet")
await handle.send("farewell")
result = await handle.future
```

## Event Timeout

Pass a `timeout` and a `timeout_step_name` to `wait_for_event()` to limit how long the workflow waits. If no event arrives before the timeout, the engine runs the named step:

```python
from datetime import timedelta

@order.start()
async def start(ctx: Context, order_id: str) -> Directive:
    ctx.store.put("order_id", order_id)
    return ctx.next.wait_for_event(
        timeout=timedelta(hours=24),
        timeout_step_name="auto_approve",
    )

@order.step()
async def auto_approve(ctx: Context) -> Directive:
    ctx.logger.info("No approval received — auto-approving")
    ctx.store.put("status", "auto-approved")
    return ctx.next.complete("auto-approved")
```

If an event does arrive before the timeout, the timeout step is not run.

## Reference

### `@workflow.event()`

| Parameter | Type | Default | Description |
|---|---|---|---|
| `name` | `str \| None` | `None` | Event name. Defaults to the function name. |

Handler signature:

```python
async def handler(ctx: Context, [**payload_kwargs]) -> Directive
```

### `ctx.next.wait_for_event()`

| Parameter | Type | Default | Description |
|---|---|---|---|
| `timeout` | `timedelta \| None` | `None` | How long to wait before running the timeout step. |
| `timeout_step_name` | `str \| None` | `None` | Step to run if the timeout fires. Required when `timeout` is set. |

### `handle.send()`

| Parameter | Type | Default | Description |
|---|---|---|---|
| `event_name` | `str` | — | Name of the event to dispatch. Must match a registered `@workflow.event()` handler. |
| `payload` | `Any \| None` | `None` | Optional data passed to the handler. Dicts are unpacked as kwargs; scalars are passed as a positional argument. |
