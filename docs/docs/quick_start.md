---
title: Quick Start
---

You can quickly scaffold a new Ground Control project by running our starter script. This will download the project into a `grctl-starter` directory.

```bash
curl -LsSf https://raw.githubusercontent.com/cemevren/grctl-starter/main/init.sh | sh
```

Once the project is created, navigate into it:

```bash
cd grctl-starter
```

### Next Steps

We recommend using <a href="https://mise.jdx.dev" target="_blank">`mise`</a> to manage the required tools (Python, uv, and the grctl CLI). 

**1. Install project tools and dependencies:**

```bash
mise install
```

**2. Start the `grctld` server** (in your current terminal or a background tmux session):

```bash
grctld
```
*(Optionally run this in the background: `tmux new-session -d -s grctl_server 'grctld'`)*

**3. Start the worker** (in a new terminal):

```bash
mise run worker
```

**4. Trigger a workflow** (in another terminal):

```bash
grctl workflow start --type Hello --input '{"name": "World"}'
```

### Manual Installation (Without `mise`)

If you prefer not to use `mise`, you can install the components manually:

**1. Install the server and CLI:**
```bash
curl -LsSf https://raw.githubusercontent.com/cemevren/grctl/sdk/packaging/install.sh | sh
```

**2. Start the `grctld` server** (in your current terminal or a background tmux session):
```bash
grctld
```

**3. Install project dependencies:**
```bash
uv sync
```

**4. Start the worker** (in a new terminal):
```bash
uv run python worker.py
```

**5. Trigger a workflow** (in another terminal):
```bash
grctl workflow start --type Hello --input '{"name": "World"}'
```

## Project Structure

```
.
├── pyproject.toml       # Project config and dependencies
├── worker.py            # Worker entry point — registers and runs workflows
├── client.py            # Client entry point — starts a workflow and prints the result
├── workflows/
│   └── hello.py         # Sample "Hello World" workflow definition
└── README.md
```

## What's in the sample workflow?

`workflows/hello.py` defines a simple workflow that:

1. Receives a `name` input
2. Calls a greeting task
3. Stores the result and completes

This demonstrates the core grctl concepts: **workflows**, **tasks**, **context/store**, and **directives**.

## Next Steps

- Add more workflows in the `workflows/` directory
- Explore [Ground Control documentation](https://cemevren.github.io/grctl/)
