# Ground Control

Ground Control is a lightweight workflow orchestration engine built for fail-safe execution. Coordinate microservices, background tasks, AI agents, and edge services with built-in durability, retries, and fault tolerance—built on [NATS.io](https://nats.io).

> [!WARNING]
> **Status: Pre-alpha**  
> The API is currently unstable and subject to change.

**[Documentation](https://cemevren.github.io/grctl/)** • **[Quick Start](https://cemevren.github.io/grctl/quick_start/)** 

## Why Ground Control?

When your processes span multiple services—sequences of API calls, database writes, and external requests—wiring them with traditional queues and manual retry logic is brittle. A worker crash midway often leads to inconsistent state and complex debugging.

Ground Control replaces that manual infrastructure. You write your process as a straight-line workflow, and the engine records each step's result as it happens. If a worker crashes, another picks up and resumes from the last successful step. State is always consistent: either a step's results are fully saved, or not at all.

## Key Features

- **Step-Based Orchestration**: Organizes logic around `Steps`, providing clear transaction boundaries and performance benefits over task-only engines.
- **Stateless Workers**: Workers only execute your code. The state machine runs on the server, making workers easy to scale and manage.
- **NATS-Native Architecture**: Uses NATS JetStream for high-throughput, append-only persistence. No traditional relational database required.
- **Distributed by Design**: Works seamlessly across geographic locations, edge devices, and multi-region cloud environments.
- **Single Binary**: The Go server can run as a single binary with no external dependencies (embedded NATS).

## What it Looks Like

Ground Control uses a clean, decorator-based API similar to FastAPI.

```python
from grctl.workflow import Workflow
from grctl.worker.task import task
from grctl.worker.context import Context
from grctl.models import Directive

order = Workflow(workflow_type="ProcessOrder")

@task
async def charge_payment(order_id: str, amount: float) -> str:
    return await payment_gateway.charge(order_id, amount)

@order.start()
async def start(ctx: Context, order_id: str, amount: float) -> Directive:
    ctx.store.put("order_id", order_id)
    
    # Tasks are recorded in history and skipped on replay
    payment_id = await charge_payment(order_id, amount)
    ctx.store.put("payment_id", payment_id)

    return ctx.next.step(ship)

@order.step()
async def ship(ctx: Context) -> Directive:
    order_id = await ctx.store.get("order_id", str)
    
    # Deterministic utilities for replay-safe execution
    tracking_id = await ctx.uuid4()
    await create_shipment(order_id, str(tracking_id))

    return ctx.next.complete({"tracking_id": str(tracking_id)})
```

## Contributing

Contributions are welcome! Please read our [Contributing Guide](CONTRIBUTING.md) and [Security Policy](SECURITY.md) for more information.

## License

This project is licensed under the Apache License 2.0 - see the [LICENSE](LICENSE) file for details.
