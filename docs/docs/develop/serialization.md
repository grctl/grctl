---
title: Serialization
---

All data that crosses a persistence boundary. Workflow inputs, task arguments, store values, event payloads, is serialized using [msgpack](https://msgpack.org/) via the [msgspec](https://jcristharris.com/msgspec/) library.

## Supported Types

### Built-in Types

These types work out of the box with no extra configuration:

| Type | Examples |
|---|---|
| Primitives | `str`, `int`, `float`, `bool`, `None` |
| Collections | `list`, `dict`, `tuple`, `set` |
| Dataclasses | `@dataclasses.dataclass` |
| msgspec Structs | `msgspec.Struct` subclasses |

```python
from dataclasses import dataclass

@dataclass
class OrderItem:
    sku: str
    qty: int
    price: float

@order.start()
async def start(ctx: Context, order_id: str, items: list[OrderItem]) -> Directive:
    ctx.store.put("items", items)
    return ctx.next.step(process)
```

The client passes the data in `workflow_input`. The engine deserializes each key to the handler's parameter types:

```python
result = await client.run_workflow(
    type="ProcessOrder",
    id="order-123",
    input={
        "order_id": "abc",
        "items": [
            OrderItem(sku="WIDGET-1", qty=2, price=9.99),
            OrderItem(sku="GADGET-3", qty=1, price=24.50),
        ],
    },
)
```

Dataclasses are natively supported by msgspec. No custom codec registration is needed.

### Pydantic Models

Pydantic `BaseModel` subclasses are supported through a built-in codec handler. Models are serialized with `model_dump()` and reconstructed with `model_validate()`, so all Pydantic validation runs on deserialization.

```python
from pydantic import BaseModel

class Payment(BaseModel):
    amount: float
    currency: str = "USD"

@order.start()
async def start(ctx: Context, payment: Payment) -> Directive:
    ctx.store.put("payment", payment)
    return ctx.next.step(charge)

@order.step()
async def charge(ctx: Context) -> Directive:
    payment = await ctx.store.get("payment", Payment)  # returns Payment instance
    ...
```

Nested Pydantic models work as expected: `model_dump()` and `model_validate()` handle the recursion.

## Where Serialization Applies

Serialization happens at every persistence boundary in the system:

| Boundary | What gets serialized |
|---|---|
| **Workflow input** | The `workflow_input` dict passed by the client. Each key is deserialized to the start handler's parameter types. |
| **Store** | Values written with `ctx.store.put()`. Deserialized on `ctx.store.get()`. |
| **Task args and results** | Task inputs and outputs are serialized to the workflow history. See [Tasks](tasks.md). |
| **Event payloads** | Data sent with `handle.send()`. Deserialized to the event handler's parameter types. See [Events](events.md). |

### Typed Store Access

`ctx.store.get()` accepts an optional type argument. Without it, you get the raw deserialized value (dicts, lists, primitives). With it, the value is converted to the target type:

```python
# Untyped — returns a plain dict
raw = await ctx.store.get("payment")

# Typed — returns a Payment instance
payment = await ctx.store.get("payment", Payment)
```

This is especially important for Pydantic models: without the type argument, you get a dict, not a model instance.

## Custom Types

Register custom type handlers via the `CodecRegistry` on the client:

```python
from grctl.client import Client
from grctl.worker.codec import CodecRegistry

codec = CodecRegistry()

codec.register(
    check=lambda tp: issubclass(tp, Money),
    encode=lambda obj: {"amount": obj.amount, "currency": obj.currency},
    decode=lambda tp, data: tp(amount=data["amount"], currency=data["currency"]),
)

client = Client(connection, codec=codec)
```

Each handler is a triple of:

| Function | Signature | Purpose |
|---|---|---|
| `check` | `(type) -> bool` | Returns `True` if this handler applies to the type |
| `encode` | `(obj) -> Any` | Converts the object to a msgpack-serializable form |
| `decode` | `(type, data) -> Any` | Reconstructs the object from deserialized data |

Handlers are checked in LIFO order. The last registered handler that matches wins. This lets you override the built-in Pydantic handler for specific subtypes if needed.

## How It Works

When a value is encoded (e.g., `ctx.store.put()`):

1. msgspec attempts to encode the value directly
2. If the type is not natively supported, msgspec calls the codec's `enc_hook`
3. `enc_hook` walks registered handlers (LIFO) until one matches
4. The handler's `encode` function converts the object to a serializable form
5. msgspec encodes the result as msgpack bytes

When a value is decoded with a target type (e.g., `ctx.store.get(key, MyType)`):

1. msgspec decodes the raw bytes into primitives (dicts, lists, etc.)
2. `msgspec.convert()` attempts to convert the primitives to the target type
3. For types that need custom handling, it calls the codec's `dec_hook`
4. The handler's `decode` function reconstructs the typed object

Primitives and dataclasses skip the hook entirely. Msgspec handles them natively.
