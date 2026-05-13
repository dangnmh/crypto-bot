# Crypto-Bot AI Assistant Guidelines

Welcome to the `crypto-bot` project. As an AI coding assistant, you are expected to rigorously adhere to the following architectural, coding, and testing conventions when generating, modifying, or reviewing code.

## 1. Project Context
This is a High-Frequency Trading (HFT) cryptocurrency bot written in Go. Reliability, extreme precision, determinism, and high test coverage are mandatory.

## 2. Architecture & Design (Domain-Driven Design)
We strictly follow Clean Architecture. For full architectural details, see [`docs/tech/architecture.md`](docs/tech/architecture.md).

## 3. Pre-Commit Quality Gates
Before suggesting code is complete, ensure it passes the quality gates.
**IMPORTANT**: You MUST ALWAYS run these terminal commands via WSL using `wsl -d Ubuntu bash -c "..."`. For example:
- `wsl -d Ubuntu bash -c "go mod tidy"`
- `wsl -d Ubuntu bash -c "make lint"` (runs `golangci-lint`, including `goimports` formatting)
- `wsl -d Ubuntu bash -c "make test"` (runs with race detector)
- `wsl -d Ubuntu bash -c "make lint && make test"`

*When in doubt, refer to [`docs/tech/coding_conventions.md`](docs/tech/coding_conventions.md) and [`docs/tech/testing_conventions.md`](docs/tech/testing_conventions.md) for extended technical documentation and standards.*
