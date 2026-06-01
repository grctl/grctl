---
title: Context
---

Every workflow handler receives a `Context` as its first argument. It provides access to workflow state storage, step transitions, deterministic utilities, and logging.

```python
from grctl.worker.context import Context
from grctl.models import Directive

@order.step()
async def process(ctx: Context) -> Directive:
    order_id = await ctx.store.get("order_id")
    # ... do work ...
    return ctx.next.complete({"order_id": order_id})
```

## Store

`ctx.store` is a key-value store scoped to the workflow run. Use it to pass state between steps.

```python
@order.start()
async def start(ctx: Context, order_id: str, amount: float) -> Directive:
    ctx.store.put("order_id", order_id)
    ctx.store.put("amount", amount)
    return ctx.next.step(validate)

@order.step()
async def validate(ctx: Context) -> Directive:
    order_id = await ctx.store.get("order_id", str)  # raises StoreKeyNotFoundError if not set
    amount = await ctx.store.get("amount", float)
    return ctx.next.step(charge)
```

!!! info "Handling Missing Keys"
    If a key is not found in the store, `await ctx.store.get(key)` raises `StoreKeyNotFoundError` (which inherits from the standard Python `KeyError`).

    You can import it from `grctl.worker` to handle missing keys:

    ```python
    from grctl.worker import StoreKeyNotFoundError

    try:
        await ctx.store.get("task_a_result", str)
    except StoreKeyNotFoundError:
        # Pause the workflow and wait for the task to complete
        return ctx.next.wait()
    ```


### Atomicity and Buffering

The Store provides an **all-or-nothing guarantee** for state changes:

- **Writes are buffered**: `ctx.store.put()` writes to local memory immediately but does not hit the server during step execution. 
- **Atomic flush**: Pending updates are flushed only when the handler returns a `Directive`. The server saves the state changes, history events, and the next step assignment in a single atomic transaction.
- **Consistency**: This ensures that state never drifts from history. If a step's store updates are visible, its history is too. If a worker crashes mid-step, no partial state is ever saved, and the next worker will replay the step from the last clean state.

### Store API

| Method | Description |
|---|---|
| `ctx.store.put(key, value)` | Write a value. Buffered until step completes. |
| `await ctx.store.get(key)` | Read a value. Raises `StoreKeyNotFoundError` if not found. |
| `await ctx.store.get(key, ty)` | Read a value and convert/validate to type `ty`. Raises `StoreKeyNotFoundError` if not found. |

Values can be any msgpack-serializable primitive (strings, numbers, booleans, lists, dicts, `None`), as well as **`msgspec.Struct`** classes and **Pydantic `BaseModel`** models.

## Step Transitions

`ctx.next` is a builder that constructs the `Directive` returned from a handler. Each method produces a directive that tells the engine what to do after the current step completes.

!!! info "Step Boundaries"
    A `Directive` marks a transaction boundary. Ground Control only persists your changes and moves to the next phase when a directive is returned.

```python
return ctx.next.step(next_step_fn)           # run another step
return ctx.next.wait()                       # pause, wait for an event
return ctx.next.complete(result)             # finish the workflow
return ctx.next.fail(error)                  # fail the workflow
```

See [Workflows](workflows.md) for the full transition table and [Events](events.md) for `wait` details.

## Deterministic Functions

Unlike full-replay engines, Ground Control only re-executes the latest step — not the entire workflow — when a worker picks up a run after a crash. Any non-deterministic calls within a step, getting the current time, generating a UUID, sampling a random number, must go through `ctx` so the engine can record the value on first execution and replay the same value if the step is retried.

**Do not use `datetime.now()`, `uuid.uuid4()`, or `random.random()` directly inside handlers or tasks.** Use the `ctx` equivalents instead.

### `ctx.now()`

Returns the current UTC time as a `datetime`. On re-execution, returns the recorded value from the first execution.

```python
from datetime import datetime

@order.step()
async def record_timestamp(ctx: Context) -> Directive:
    created_at = await ctx.now()  # datetime, UTC
    ctx.store.put("created_at", created_at.isoformat())
    return ctx.next.complete("done")
```

### `ctx.random()`

Returns a float in `[0.0, 1.0)`. On re-execution, returns the recorded value.

```python
@order.step()
async def assign_variant(ctx: Context) -> Directive:
    roll = await ctx.random()
    variant = "A" if roll < 0.5 else "B"
    ctx.store.put("variant", variant)
    return ctx.next.step(run_experiment)
```

### `ctx.uuid4()`

Returns a `uuid.UUID`. On re-execution, returns the recorded value.

```python
@order.step()
async def create_record(ctx: Context) -> Directive:
    record_id = await ctx.uuid4()
    ctx.store.put("record_id", str(record_id))
    return ctx.next.step(save_record)
```

### `ctx.sleep(duration)`

Pauses execution for the given duration. On re-execution, returns immediately — the sleep is not repeated.

```python
from datetime import timedelta

@order.step()
async def wait_and_retry(ctx: Context) -> Directive:
    await ctx.sleep(timedelta(minutes=5))
    return ctx.next.step(retry_payment)
```

`ctx.sleep` is appropriate for short delays inside a step. For pausing a workflow until an external event occurs, use `ctx.next.wait()` instead.

## Logger

`ctx.logger` is a standard Python logger that is replay-aware. Log calls are suppressed during re-execution so that log output is not duplicated.

```python
@order.step()
async def process(ctx: Context) -> Directive:
    ctx.logger.info("Processing order %s", await ctx.store.get("order_id"))
    return ctx.next.complete("done")
```

The logger supports the standard `logging` interface: `debug`, `info`, `warning`, `error`, `exception`.

## Run Info

`ctx.run` exposes metadata about the current workflow run. Useful for logging or passing the workflow ID to external systems.

```python
@order.step()
async def log_run(ctx: Context) -> Directive:
    ctx.logger.info("run_id=%s wf_id=%s", ctx.run.id, ctx.run.wf_id)
    return ctx.next.complete("done")
```

| Field | Type | Description |
|---|---|---|
| `ctx.run.id` | `str` | Unique run ID (ULID) |
| `ctx.run.wf_id` | `str` | Workflow ID (caller-supplied, used for deduplication) |
| `ctx.run.wf_type` | `str` | Workflow type string |

## Child Workflows

`ctx.start()` launches a child workflow and returns a `WorkflowHandle`. `ctx.send_to_parent()` emits an event to the parent workflow. Both are covered in [Child Workflows](child-workflows.md).

## Reference

### Context Properties

| Property | Type | Description |
|---|---|---|
| `ctx.store` | `Store` | Key-value store for this run |
| `ctx.next` | `NextBuilder` | Step transition builder |
| `ctx.logger` | `ReplayAwareLogger` | Replay-aware logger |
| `ctx.run` | `RunInfo` | Metadata about the current run |

### Deterministic Functions

| Method | Returns | Description |
|---|---|---|
| `await ctx.now()` | `datetime` | Current UTC time, recorded on first call |
| `await ctx.random()` | `float` | Random float in `[0.0, 1.0)`, recorded on first call |
| `await ctx.uuid4()` | `uuid.UUID` | Random UUID v4, recorded on first call |
| `await ctx.sleep(duration)` | `None` | Sleep for `timedelta`; skipped on re-execution |
