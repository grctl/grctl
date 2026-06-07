---
title: Child Workflows
---

A workflow can launch other workflows as children. The parent and child run independently — each has its own state and step execution. There are three ways to coordinate:

| Method | When to use |
|---|---|
| `ctx.start_child()` | Fire-and-forget, or manual coordination via `ctx.send_to_parent()` |
| `ctx.start_child(on_completed_step=...)` | Server-triggered callback when child reaches a terminal state |
| `ctx.run_child()` | Block in-step until the child completes (simple sequential flows) |

## `ctx.start_child()` — fire-and-forget

Start a child and park the parent in `Wait`. The child notifies the parent by calling `ctx.send_to_parent()`, which delivers an event to the parent's inbox.

```python
@order_wf.start()
async def order_start(ctx: Context) -> Directive:
    await ctx.start_child(
        "Payment",
        workflow_id=f"payment-{ctx.run.wf_id}",
        workflow_input={"amount": 99.0},
        workflow_timeout=timedelta(minutes=5),
    )
    return ctx.next.wait()

@order_wf.event(name="payment_completed")
async def on_payment_done(ctx: Context, txn_id: str) -> Directive:
    return ctx.next.complete(txn_id)
```

The child sends its result back:

```python
@payment_wf.step()
async def payment_done(ctx: Context) -> Directive:
    txn_id = await ctx.store.get("txn_id")
    await ctx.send_to_parent("payment_completed", payload={"txn_id": txn_id})
    return ctx.next.complete(txn_id)
```

`ctx.start_child()` returns a `WorkflowHandle`. The `workflow_id` acts as a deduplication key — if the parent's step re-executes (e.g. after a worker restart), the child is not started twice.

## `ctx.start_child(on_completed_step=...)` — server-triggered callback

Pass `on_completed_step` to have the server automatically trigger a parent step when the child reaches any terminal state (completed, failed, or cancelled). The parent still parks with `ctx.next.wait()`.

```python
@order_wf.start()
async def order_start(ctx: Context) -> Directive:
    await ctx.start_child(
        "Payment",
        workflow_id=f"payment-{ctx.run.wf_id}",
        workflow_input={"amount": 99.0},
        on_completed_step=on_payment_done,
    )
    return ctx.next.wait()

@order_wf.step()
async def on_payment_done(ctx: Context, outcome: ChildOutcome) -> Directive:
    if not outcome.ok:
        return ctx.next.fail(ErrorDetails(message=outcome.error.message))
    return ctx.next.complete(outcome.result)
```

`ChildOutcome` carries the terminal state of the child. Parameterize it to decode the result into a known type:

```python
async def on_payment_done(ctx: Context, outcome: ChildOutcome[PaymentResult]) -> Directive:
    result: PaymentResult = outcome.unwrap()  # raises WorkflowError on failure
    ...
```

`outcome.unwrap()` returns the result on success or raises `WorkflowError` on failure — useful when the callback only cares about the happy path.

## `ctx.run_child()` — block until complete

Start a child and await its result in the same step. This is the simplest pattern when the parent's flow is purely sequential.

```python
@order_wf.start()
async def order_start(ctx: Context) -> Directive:
    result = await ctx.run_child(
        "Payment",
        workflow_id=f"payment-{ctx.run.wf_id}",
        workflow_input={"amount": 99.0},
        workflow_timeout=timedelta(minutes=5),
    )
    return ctx.next.complete(result)
```

Raises `WorkflowError` if the child fails, `asyncio.CancelledError` if it is cancelled or terminated, and `TimeoutError` if the `timeout` parameter elapses before the child finishes.

The optional `timeout` parameter is a client-side deadline in seconds, independent of `workflow_timeout`:

```python
result = await ctx.run_child("Payment", workflow_id="pay-1", timeout=30.0)
```

## Sending an Event to the Parent

`ctx.send_to_parent()` is a general-purpose way to send any event to the parent — not just a final result. A child can call it multiple times across different steps to report progress, intermediate results, or status changes. The parent handles each event through its normal `@workflow.event()` handlers.

```python
@payment_wf.step()
async def payment_authorised(ctx: Context) -> Directive:
    await ctx.send_to_parent("payment_authorised", payload={"status": "authorised"})
    return ctx.next.step(payment_capture)

@payment_wf.step()
async def payment_capture(ctx: Context) -> Directive:
    txn_id = await ctx.store.get("txn_id")
    await ctx.send_to_parent("payment_completed", payload={"txn_id": txn_id})
    return ctx.next.complete(txn_id)
```

The payload dict is unpacked as keyword arguments into the parent's event handler. The handler's parameter names must match the payload keys.

If the parent is in `Wait` state the event dispatches immediately; otherwise it waits in the inbox until the parent transitions to `Wait`.

Raises `RuntimeError` if the workflow has no parent.

## Complete Example

```python
payment_wf = Workflow(workflow_type="Payment")
order_wf = Workflow(workflow_type="Order")

# --- Child workflow ---

@payment_wf.start()
async def payment_start(ctx: Context, amount: float) -> Directive:
    ctx.store.put("amount", amount)
    return ctx.next.step(payment_process)

@payment_wf.step()
async def payment_process(ctx: Context) -> Directive:
    amount = await ctx.store.get("amount")
    txn_id = await charge_card(amount)
    ctx.store.put("txn_id", txn_id)
    return ctx.next.step(payment_done)

@payment_wf.step()
async def payment_done(ctx: Context) -> Directive:
    txn_id = await ctx.store.get("txn_id")
    await ctx.send_to_parent("payment_completed", payload={"txn_id": txn_id})
    return ctx.next.complete(txn_id)

# --- Parent workflow ---

@order_wf.start()
async def order_start(ctx: Context, order_id: str, amount: float) -> Directive:
    ctx.store.put("order_id", order_id)
    ctx.store.put("amount", amount)
    return ctx.next.wait()

@order_wf.event()
async def start_payment(ctx: Context) -> Directive:
    amount = await ctx.store.get("amount")
    order_id = await ctx.store.get("order_id")
    await ctx.start_child(
        "Payment",
        workflow_id=f"payment-{order_id}",
        workflow_input={"amount": amount},
    )
    return ctx.next.wait()

@order_wf.event(name="payment_completed")
async def on_payment_done(ctx: Context, txn_id: str) -> Directive:
    ctx.store.put("txn_id", txn_id)
    return ctx.next.complete(txn_id)
```

Register both workflows on the same worker:

```python
worker = Worker(workflows=[order_wf, payment_wf], connection=connection)
```

## Reference

### `await ctx.start_child()`

| Parameter | Type | Default | Description |
|---|---|---|---|
| `workflow_type` | `str` | — | The `workflow_type` of the child workflow to start. |
| `workflow_id` | `str` | — | Stable identifier for the child run. Used for deduplication. |
| `workflow_input` | `dict | None` | `None` | Input passed to the child's start handler. |
| `workflow_timeout` | `timedelta | None` | `None` | Maximum duration for the child workflow. |
| `on_completed_step` | `StepHandler | None` | `None` | Parent step the server triggers when the child reaches a terminal state. Receives a `ChildOutcome`. |

Returns a `WorkflowHandle` with a `run_info` field containing the child's run metadata.

### `await ctx.run_child()`

| Parameter | Type | Default | Description |
|---|---|---|---|
| `workflow_type` | `str` | — | The `workflow_type` of the child workflow to start. |
| `workflow_id` | `str` | — | Stable identifier for the child run. Used for deduplication. |
| `workflow_input` | `dict | None` | `None` | Input passed to the child's start handler. |
| `workflow_timeout` | `timedelta | None` | `None` | Maximum duration for the child workflow. |
| `timeout` | `float | None` | `None` | Client-side wait in seconds before raising `TimeoutError`. |

Returns the child's result. Raises `WorkflowError` on failure, `asyncio.CancelledError` on cancellation or termination.

### `ChildOutcome[T]`

| Field | Type | Description |
|---|---|---|
| `status` | `RunStatus` | Terminal status of the child (`completed`, `failed`, `cancelled`). |
| `result` | `T | None` | The value returned by the child's final `ctx.next.complete()`. |
| `error` | `ErrorDetails | None` | Error details when the child failed or was cancelled. |
| `ok` | `bool` (property) | `True` when `status` is `completed`. |

Call `outcome.unwrap()` to return the result or raise `WorkflowError` on failure.

### `await ctx.send_to_parent()`

| Parameter | Type | Default | Description |
|---|---|---|---|
| `event_name` | `str` | — | Name of the event to deliver to the parent. Must match a `@workflow.event()` handler on the parent. |
| `payload` | `Any | None` | `None` | Data to pass to the parent's event handler. |

Raises `RuntimeError` if the workflow has no parent.
