---
title: Overview
---

# Ground Control

Ground Control is a lightweight workflow orchestration engine built for fail-safe execution. Coordinate microservices, background tasks, AI agents, and edge services with built-in durability, retries, and fault tolerance. built on [NATS.io](https://nats.io).

!!! warning "Status: Pre-alpha"

    The API is currently unstable and subject to change.

## Why

You have a process that spans multiple services. A sequence of API calls, database writes, and external requests that need to complete together. Today you're wiring this with queues, retry logic, state tracking, and idempotency checks spread across services. It works, until a worker crashes midway. Now you're debugging partial state, writing compensation logic, and building monitoring to catch the cases your retry logic doesn't cover.

Ground Control replaces that infrastructure. You write the process as a straight-line function. The engine records each step's result as it happens. If a worker crashes, another picks up and completed work is not re-executed. State is always consistent, either a step's results are fully saved or not at all.

## What it looks like
Ground Control uses a clean, decorator-based API similar to FastAPI, organizing logic into Workflows, Tasks, and Steps.

```python {filename="process_order.py"}
payment_wf = Workflow(workflow_type="Payment")
order_wf = Workflow(workflow_type="Order")


# A Task is a unit of work within a step, marked with @task.
# Tasks are recorded in history. If a step re-executes after a crash,
# completed tasks return their recorded result instead of running again.
@task
async def validate_order(order_id: str, amount: float) -> tuple[str, float]:
    await inventory_service.check(order_id)
    return order_id, amount

@task
async def process_payment(amount: float) -> str:
    return await payment_gateway.charge(amount)

@task
async def create_shipment(order_id: str, tracking_id: str) -> None:
    await shipping_service.create(order_id, tracking_id)


# ── Payment workflow (child) ──────────────────────────────────────────────────

@payment_wf.start()
async def payment_start(ctx: Context, amount: float) -> Directive:
    ctx.store.put("amount", amount)
    return ctx.next.step(payment_process_step)


@payment_wf.step()
async def payment_process_step(ctx: Context) -> Directive:
    amount = await ctx.store.get("amount", float)
    transaction_id = await process_payment(amount)

    # Send result back to the parent before completing
    await ctx.send_to_parent(
        event_name="payment_completed",
        payload={"status": "success", "transaction_id": transaction_id},
    )
    return ctx.next.complete({"transaction_id": transaction_id})


# ── Order workflow (parent) ───────────────────────────────────────────────────

# A Step is a transaction boundary within a workflow.
# Only one step runs at a time — when it completes the engine saves its outcome
# before moving on. Each step handler decides what happens next.
@order_wf.start()
async def order_start(ctx: Context, order_id: str, amount: float) -> Directive:
    validated_id, validated_amount = await validate_order(order_id, amount)
    ctx.store.put("order_id", validated_id)
    ctx.store.put("amount", validated_amount)

    # Start a child workflow and wait for it to send back an event
    payment_handle = await ctx.start(
        payment_wf.workflow_type,
        workflow_id=f"payment-{validated_id}",
        workflow_input={"amount": validated_amount},
        workflow_timeout=timedelta(minutes=5),
    )

    return ctx.next.wait()


# Event handlers are registered with @workflow.event() and resume execution
# when a matching event arrives — either from a child workflow or an external client.
@order_wf.event(name="payment_completed")
async def on_payment_completed(ctx: Context, status: str, transaction_id: str) -> Directive:
    ctx.store.put("transaction_id", transaction_id)
    ctx.logger.info(f"Payment {status}, tx={transaction_id}")
    return ctx.next.step(ship)


@order_wf.event()
async def cancel(ctx: Context, reason: str) -> Directive:
    order_id = await ctx.store.get("order_id", str)
    ctx.logger.info(f"Order {order_id} cancelled: {reason}")
    return ctx.next.complete({"status": "cancelled", "reason": reason})


@order_wf.step()
async def ship(ctx: Context) -> Directive:
    order_id = await ctx.store.get("order_id", str)

    # Deterministic helpers (ctx.now, ctx.uuid4, ctx.sleep, ctx.random) wrap
    # non-deterministic calls — their results are recorded so replays stay consistent.
    shipped_at = await ctx.now()
    tracking_id = await ctx.uuid4()

    await create_shipment(order_id, str(tracking_id))
    ctx.store.put("shipped_at", shipped_at.isoformat())

    return ctx.next.complete({"tracking_id": str(tracking_id)})
```

## The Ground Control Difference

-   **Step-Based Orchestration**: Ground Control organizes logic around `Steps`. Unlike engines that queue individual tasks, Ground Control distributes step execution. This increases performance and provides a cleaner way to organize complex workflow code.
-   **Stateless Workers**: Most engines run internal state logic on workers. Ground Control does the opposite: workers only execute your code, while the engine state machine runs on the Ground Control server. This makes workers easier to scale, update, and manage.
-   **NATS-Native Architecture**: Instead of relying on traditional relational databases, Ground Control uses NATS JetStream. This provides high write throughput for event-heavy workflows and allows Ground Control to run as a **single binary** with no external dependencies (no separate DB, no load balancers).
-   **Distributed by Design**: Built on NATS, Ground Control works seamlessly across geographic locations, edge devices, or multi-region cloud environments. If you can connect to NATS, you can run or monitor Ground Control workflows.

## Components

- **Server** — The Go server is the coordinator. It runs embedded NATS, manages the workflow state machine, and handles all state transitions. It never executes user code.
- **Worker** — Workers execute your workflow code. They connect to the server via NATS, pull step assignments, run the matching handler, and report results back. Workers hold no internal workflow state, so they can crash, restart, or scale without consequence. 
- **Client** — How an application interacts with workflows. It triggers workflow runs, sends events to running workflows, and receives results.
- **CLI** — A command-line and TUI tool for operating and inspecting active and completed workflows.
