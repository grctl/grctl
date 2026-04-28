---
title: How it works
---

## Concepts

**Workflow:** a sequence of steps that runs to completion. If the process dies halfway through, execution resumes where it left off.

**Step:** is one unit of work within a workflow. Steps run one at a time, in order. When a step finishes, its outcome is saved before the next one begins. This is the transaction boundary, the point where progress becomes permanent.

**Task** is a side-effecting operation inside a step. An API call, a database write, a message send. Tasks are [recorded to history](../develop/tasks.md) so they aren't re-executed on replay.

**History:** is an append-only log of everything that happened during a workflow run. Task results, timestamps, generated IDs. It's what makes replay possible: when a step re-executes after a crash, the engine checks history first and returns recorded results instead of running the operation again.

**Store:** is a key-value map scoped to a workflow run. Steps use it to [pass state forward](../develop/context.md#store). One step writes a value, the next reads it. Store updates are saved atomically with history, so state and history never drift apart.

**Server:** is the coordinator. It manages a state machine for each workflow run, persists history and store via NATS JetStream, and dispatches step assignments to workers. It never executes user code.

**Worker:** is a stateless process that [executes steps](../develop/worker.md). It pulls assignments, runs your handler, and reports the result. Any worker can handle any step for any workflow type it has registered.

**Client** is how external code [interacts with workflows](../develop/client.md). Starting runs, sending events, waiting for results.

## Step Execution Model

A workflow run is an isolated entity with private state and a single inbox. Directives arrive in the inbox; the run processes them one at a time, advances to a new state, and waits for the next directive. Many runs exist concurrently, but each run is independent — they share no memory and do not coordinate.

**State machine**

Every run is always in exactly one state. Non-terminal states mean execution continues:

- `Start` — the run was just created
- `Step` — a step handler is executing
- `Sleep` / `SleepUntil` — waiting for a timer
- `WaitEvent` — waiting for an external event

Terminal states end the run: `Complete`, `Fail`, `Cancel`, `Terminate`.

A transition moves the run from one state to the next as a single atomic unit: the previous state is recorded as completed, the next state is set up, and any state changes from the step are saved together. Either the whole transition lands or none of it does. The run is never observed in a half-changed state.

**Directives drive transitions**

Transitions are not initiated by the run itself. They are caused by directives delivered to the run's inbox. A directive describes what just finished and what should happen next. There are three sources:

1. **A worker finishing a step.** The handler returns a `ctx.next` decision, and the worker sends a directive: "the step completed, transition to `Sleep` / `WaitEvent` / next `Step` / `Complete`."
2. **A timer firing.** When a `Sleep` expires, the server itself produces a directive: "the sleep completed, transition to the next step."
3. **An event arriving.** When the run is in `WaitEvent` and a matching event is delivered, the server produces a directive: "the wait completed, transition to a step that handles this event."

From the run's point of view, all three look the same: a directive arrived, apply the transition.

**One directive at a time**

A run processes directives serially. If two directives target the same run concurrently — for example, a timer firing at the same moment an event arrives — only one is applied; the other is rejected and retried against the new state. This is enforced by compare-and-swap on the run's state and is what keeps the run's state and history from drifting apart, no matter how much concurrency surrounds it.

The result is a model that is easy to reason about: each run is a small, independent state machine that reacts to messages, holds its own state, and changes state only through atomic transitions.

## Lifecycle of a Workflow

![State Machine](./img/state_machine.svg)

Using the ProcessOrder workflow as an example, here's what happens end to end:

**1. Client starts the workflow:** The client sends a start request to the server. The server checks that no active run already exists for this workflow ID, creates the run, and assigns the `start` step to a worker.

**2. Worker executes the start step:** A worker picks up the step assignment and runs the `start` handler. Tasks execute in parallel via `asyncio.gather()`. Each task's result is recorded to history as it completes. When the handler returns `ctx.next.step(ship)`, the worker sends the step result back to the server — including any state changes and the next step to run.

**3. Server saves the result and dispatches the next step**: The server saves the step result atomically — state changes, history, and the next step assignment are all persisted together. Since the next step is `ship`, the server assigns it to a worker.

**4. Worker executes the ship step**: A worker (possibly a different one) picks up the `ship` step. It calls `ctx.now()` and `ctx.uuid4()` — these are recorded in history for deterministic replay. The handler returns `ctx.next.complete(...)`, and the worker sends the result back to the server.

**5. Server completes the workflow**: The server saves the final step result, transitions the workflow to the completed state, and records the workflow result.

**6. Client receives the result**: The client receives the result and returns it to the caller.

## Durable Execution

If a worker crashes halfway through a step, another worker picks it up and the step produces the same result, as if the crash never happened. No work is duplicated, no state is lost. This is what "durable execution" means in Ground Control.

It's achieved through two mechanisms:

### 1. Recording and Replay

Every non-deterministic operation (task completion, time lookup, UUID generation) is recorded in a **history log**. 

- **Recording**: When a worker executes an operation for the first time, it assigns it a deterministic ID and records the result.
- **Replay**: If a step re-executes (e.g., after a crash), the worker fetches the history first. As the handler runs, the engine replays history in order against the user code. Each task and deterministic function call is matched to its recorded result in sequence. Recorded operations return their result immediately instead of re-executing the actual work (like hitting an external API).

This mechanism makes step execution **idempotent**: running the same step multiple times always produces the same outcome.

### 2. Atomic Persistence

To ensure that the workflow's internal state (the Store) never drifts from its execution history, Ground Control uses **atomic batch updates**.

When a step completes, the worker sends a single package to the server containing:
- All pending **Store (KV) Updates**.
- The **Next Step Assignment** (the directive).

The server saves this entire batch in a single atomic operation via NATS JetStream. Either everything is saved or nothing is. This eliminates the risk of "partial success" where a task is recorded as finished but its state changes are lost.

## How we use JetStream

Ground Control uses NATS JetStream for two things: persistence and work distribution.

**Persistence through streams**

JetStream streams are append-only, replicated logs. A natural fit for workflow state, which is also append-only. All workflow data, history events, store updates, step results, run state, lives in a single stream. JetStream replicates the stream across cluster members using Raft consensus, so data survives node failures without external backups or replication logic. This gives Ground Control durable persistence and leader election without needing a separate database or coordination service.

**Work distribution through durable consumer groups**

Step assignments are delivered to workers via JetStream durable consumer groups. Each workflow type has its own consumer group, so adding more workers increases throughput without coordination. A message is acknowledged only after the worker finishes the step and publishes the result. While a step runs, the worker periodically signals progress to prevent NATS from assuming it's dead. If a worker crashes before acknowledging, NATS redelivers to another worker in the group. No step is silently lost.

## Failure and Recovery

![Execution Model](./img/execution_model.svg "Queues are based on Jetstream and uses durable consumer groups")

**Worker crashes**

When a worker crashes mid-step, NATS redelivers the unacknowledged message to another worker in the consumer group. The new worker re-executes the step handler from the beginning. What makes this safe is the replay mechanism described in [History](#history): completed tasks return their recorded results instead of running again, and deterministic functions return their original values. The step produces the same outcome it would have without the crash.

**Task failures**

When a task raises an exception, the runtime retries it according to the task's [retry policy](../develop/tasks.md#retry-policy) if one is configured. If all retries are exhausted (or no retries are configured), the exception propagates to the step handler. User code can catch it with a normal try/except — for example, to fall back to a different path or complete the workflow with an error result. If the exception goes uncaught, the step fails, which fails the entire workflow.

**Step timeouts**

Each step has a timeout enforced by the server. If a step doesn't complete within the timeout, the server fails the workflow with a `StepTimeout` error. This prevents stuck workers from blocking a workflow indefinitely.

**Server crashes**

The server's state is fully persisted in JetStream. If a server crashes, there is no state to recover, it's already in the stream. In a multi-server deployment, JetStream runs Raft consensus across the cluster. When a server goes down, a new leader is elected for the stream and the cluster continues operating without interruption.

## Concurrency and Ordering

**One step at a time**

Only one step executes per workflow run at any given time. Concurrency in Ground Control comes from running many workflow instances in parallel across workers. Not from parallelizing steps within a single workflow. This eliminates an entire class of race conditions and makes the execution model easy to reason about.

**Parallel tasks within a step**

Within a single step, tasks are regular async functions and can run concurrently using any asyncio mechanism — `asyncio.gather()`, `TaskGroup`, `create_task`, etc. Each task records its result to history independently. During replay, the runtime resolves concurrent tasks by their completion order. It expects them to complete in the same sequence as the original execution, matched by operation ID. This preserves determinism even when tasks run in parallel.

**Event buffering**

Events can arrive at any time, including while a step is running. When that happens, the event is saved to an inbox rather than processed immediately. The workflow only handles events when it transitions to `WaitEvent` state. At that point, the server pulls one waiting event from the inbox and dispatches it as a step. Once that step completes, the handler's `ctx.next` determines what happens next; transition to another step, wait for more events, or complete.

**State consistency**

On the server side, run state updates use compare-and-swap (CAS). If two directives for the same workflow arrive concurrently, only one succeeds — the other gets a CAS rejection and waits for the next turn. This prevents concurrent processing from corrupting the workflow state.

## Scalability Model

**Horizontal scaling**

Adding more workers for a workflow type increases throughput linearly. Each worker joins the durable consumer group and starts pulling step assignments. No coordination between workers is needed. Since workers are stateless, scaling up or down is just starting or stopping processes.

**Adding more servers**

The Go server runs embedded NATS with clustering support. Multiple server instances can form a NATS cluster, distributing the directive processing and state management load. JetStream's built-in replication ensures state durability across cluster members.

**Bottlenecks**

The main constraint is that each workflow run processes one step at a time sequentially. A single workflow run can't go faster by adding workers — it's bounded by step execution time. The system scales by running many workflow instances concurrently, not by speeding up individual workflows.

The server-side directive processing is serialized per workflow run via CAS. Under high event throughput for a single workflow, CAS rejections increase and directives spend more time waiting for their turn. This is by design. It protects state consistency, but it means a single workflow run has a natural throughput ceiling.

**Scaling beyond a single cluster**

A Ground Control cluster uses a single JetStream stream for all workflow state. A single stream has excellent write throughput, but at very high volumes it becomes the ceiling, and at that point, other per-cluster resources (KV store CAS, Raft consensus, network bandwidth) are typically saturated too.

Rather than supporting multiple streams within one cluster, Ground Control's scaling model is **one stream per cluster**. When workload exceeds what a single cluster can handle, deploy additional clusters and assign workflow types across them. Each cluster is fully independent, its own NATS cluster, its own stream, its own workers and server instances. This gives complete isolation: a misbehaving workflow type in one cluster cannot starve workflows in another, and each cluster scales independently.

This approach keeps the server simple (no multi-stream routing, no cross-stream coordination) and matches how operators already think about infrastructure isolation. Clients that need to interact with workflows across clusters connect to the appropriate cluster for each workflow type.
