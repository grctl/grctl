# Contributing to Ground Control

First off, thank you for considering contributing to Ground Control! It's people like you that make Ground Control such a great tool.

We value reliability, predictability, and battle-tested patterns over clever solutions.

## Our Development Philosophy

1.  **Reliability First**: Every change must maintain or improve the stability of the system.
2.  **Surgical Changes**: We prefer small, focused Pull Requests that do one thing well.
3.  **Design for Failure**: Assume everything that can fail will fail. How does your change handle it?
4.  **No Shortcuts**: We don't cut corners on testing, types, or documentation.

## How Can I Contribute?

### Reporting Bugs

-   **Check for existing issues**: Someone might have already reported it.
-   **Use a clear title**: Describe the problem concisely.
-   **Provide a reproduction**: A minimal script or steps to reproduce the issue is the fastest way to get it fixed.

### Suggesting Enhancements

-   **Explain the use case**: Why is this feature needed? What problem does it solve?
-   **Keep it simple**: We prefer general-purpose primitives over niche features.

### Pull Requests

**Don't create a Pull Request directly. Create an issue first and discuss the potential implementations first.**

All Pull Request(s) must contain an issue ID in the title or description (e.g., `fixes #123`). PRs that do not contain an issue ID will be automatically rejected.

1.  **Fork the repo** and create your branch from `main`.
2.  **Install dependencies**: `go mod download`
3.  **Follow the coding standards**:
    -   Run `mise run lint` and `mise run fmt`.
    -   Ensure all tests pass: `mise run test`.
4.  **Add tests**: New features or bug fixes must include tests.
5.  **Update documentation**: If you change behavior, update the relevant docs.

## License

By contributing to Ground Control, you agree that your contributions will be licensed under the [Apache License 2.0](LICENSE).
