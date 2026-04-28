---
title: Workflows
weight: 1
---

A workflow is a definition that describes a sequence of steps and how to transition between them. You define workflows using a decorator-based API similar to FastAPI route registration.

## Defining a Workflow

Create a `Workflow` instance and register handlers with decorators:

```python
from grctl.workflow import Workflow
from grctl.worker.context import Context
from grctl.models import Directive

order = Workflow(workflow_type="ProcessOrder")

@order.start()
async def start(ctx: Context, order_id: str) -> Directive:
    ctx.store.put("order_id", order_id)
    return ctx.next.step(process)

@order.step()
async def process(ctx: Context) -> Directive:
    order_id = await ctx.store.get("order_id")
    # ... do work ...
    return ctx.next.complete({"order_id": order_id, "status": "done"})
```

The `workflow_type` string identifies this workflow across the system. Clients use it to start workflows, workers use it to route step assignments.

## Workflow ID and Runs

Every workflow instance is identified by a `workflow_id`,  a stable, caller-supplied string like an order number or request ID. When a client starts a workflow, it provides this ID. The server rejects the request if a workflow with that ID is already running, which makes workflow starts idempotent.

Each time a workflow runs, the system generates a `run_id` — a unique identifier for that specific execution. A single `workflow_id` can have multiple runs over its lifetime, but only one can be active at a time.

| Concept | Set by | Purpose |
|---|---|---|
| `workflow_id` | Caller | Stable identity. Used for deduplication and to get a handle to an existing workflow. |
| `run_id` | System | Identifies a specific execution. Generated automatically on each start. |

Use `workflow_id` to get a handle to a running workflow and interact with it — send events, run queries, or wait for the result. See [Client](../client) for details.

## Start Handler

Every workflow needs exactly one start handler. It runs once when the workflow is first created, receives the workflow input as function arguments, and returns a directive that determines what happens next.

Not every workflow needs multiple steps. If the workflow is simple, the start handler alone can do all the work and complete directly:

```python
@order.start()
async def start(ctx: Context, order_id: str, amount: float) -> Directive:
    result = await charge_payment(order_id, amount)
    return ctx.next.complete(result)
```

Use separate steps when you need to split a workflow into distinct logical phases. For example, validate, then charge, then ship. Each step is a persistence boundary, so they're useful when you want progress saved between phases.

```python
@order.start()
async def start(ctx: Context, order_id: str, amount: float) -> Directive:
    # Initialize workflow state
    ctx.store.put("order_id", order_id)
    ctx.store.put("amount", amount)

    # Transition to next step
    return ctx.next.step(validate)
```

The start handler's parameters (after `ctx`) are populated from the `workflow_input` dict passed by the client. If the client sends `{"order_id": "abc", "amount": 49.99}`, those become the `order_id` and `amount` arguments.

## Steps

Steps are the building blocks of a workflow. Each step is a persistence boundary. When a step completes, its result is saved before the next step begins. Only one step runs at a time per workflow. See [How It Works](../how_it_works) for details on how steps are dispatched to workers.

```python
@order.step()
async def validate(ctx: Context) -> Directive:
    order_id = await ctx.store.get("order_id")
    # ... validate order ...
    return ctx.next.step(charge)

@order.step()
async def charge(ctx: Context) -> Directive:
    amount = await ctx.store.get("amount")
    # ... charge payment ...
    return ctx.next.complete("charged")
```

Every step handler receives a `Context` and returns a `Directive`. The directive tells the engine what to do next: run another step, wait for an event, complete, or fail.

### Step Timeouts

Steps have a default timeout of 10 seconds. Override it per step:

```python
from datetime import timedelta

@order.step(timeout=timedelta(seconds=60))
async def long_running_step(ctx: Context) -> Directive:
    # This step has 60 seconds to complete
    ...
```

If a step exceeds its timeout, the engine cancels it and fails the workflow.

### Error Handling

If a step handler raises an uncaught exception, the workflow fails with a `StepFailed` error. The exception type and message are recorded automatically so operators can see why the workflow failed; you don't need to call `ctx.next.fail()` yourself for unhandled errors.

To recover from an error instead of failing, catch the exception inside the step and use `ctx.next` to decide what happens next; transition to a recovery step or complete with a partial result.

```python
@order.step()
async def validate(ctx: Context) -> Directive:
    payload = await ctx.store.get("payload")

    try:
        amount = int(payload["amount"])
    except (KeyError, ValueError):
        return ctx.next.step(reject)

    ctx.store.put("amount", amount)
    return ctx.next.step(charge)
```

For task-level error handling — including retries and compensating actions — see [Tasks](tasks.md).

### Looping Steps

A step can transition back to itself, creating a loop. This is useful for processing items in batches with saving state between each iteration:

```python
counter = Workflow(workflow_type="Counter")

@counter.start()
async def start(ctx: Context, target: int) -> Directive:
    ctx.store.put("count", 0)
    ctx.store.put("target", target)
    return ctx.next.step(increment)

@counter.step()
async def increment(ctx: Context) -> Directive:
    count = await ctx.store.get("count")
    target = await ctx.store.get("target")

    # Do a batch of work
    for _ in range(10):
        count += 1

    ctx.store.put("count", count)

    if count >= target:
        return ctx.next.complete(count)

    # Loop back — state is persisted between iterations
    return ctx.next.step(increment)
```

Each loop iteration is a separate step execution with its own persisted state, so progress is never lost even if the worker crashes mid-loop.

## Step Transitions

Every handler returns a directive via `ctx.next` that tells the engine what to do next:

| Method | Description |
|---|---|
| `ctx.next.step(fn)` | Transition to the given step function |
| `ctx.next.wait_for_event()` | Pause and wait for external events. See [Events](events.md) for details |
| `ctx.next.complete(result)` | Complete the workflow with a result |
| `ctx.next.fail(error)` | Fail the workflow with an error |


## Events

Use `@workflow.event()` to register event handlers on a workflow. Event handlers run when an external system sends an event to a running workflow via a workflow handle. They can mutate state and control transitions just like steps.

```python
@order.event()
async def cancel(ctx: Context) -> Directive:
    ctx.store.put("status", "cancelled")
    return ctx.next.complete("cancelled")
```

!!! warning "Important"

    Events can arrive at any time, including while a step is running. When that happens, the event is saved to an inbox rather than processed immediately. The workflow only handles events when it transitions to `WaitEvent` state. At that point, the server pulls one waiting event from the inbox and dispatches it as a step. Once that step completes, the handler's `ctx.next` determines what happens next; transition to another step, wait for more events, or complete.

Events are covered in depth in [Events](events.md).

## Reference

### Workflow

| Method | Parameters | Description |
|----|----|----|
| `Workflow(workflow_type)` | `workflow_type: str` | Create a workflow definition |
| `@workflow.start()` | — | Register the start handler (exactly one) |
| `@workflow.step()` | `timeout: timedelta \| None`, `retry_policy: RetryPolicy \| None` | Register a step handler |
| `@workflow.event()` | `name: str \| None` | Register an event handler |
| `@workflow.query()` | `name: str \| None` | Register a query handler |

### Handler Signatures

| Handler | Signature | Returns |
|---|---|---|
| Start | `async def fn(ctx: Context, **input) -> Directive` | Directive |
| Step | `async def fn(ctx: Context) -> Directive` | Directive |
| Event | `async def fn(ctx: Context, [payload]) -> Directive` | Directive |
| Query | `async def fn(ctx: Context) -> Any` | Any value |

### Step Defaults

| Setting | Default |
|----|------|
| `timeout` | 10 seconds |
| `retry_policy` | None (no retries, single attempt) |
