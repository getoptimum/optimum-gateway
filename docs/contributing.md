# Contributing to optimum-gateway

Thank you for your interest in contributing! We want to make it as easy as possible for you to get your code into the project. For more detailed documentation, please refer to our [Official Guide](https://getoptimum.github.io/optimum-gateway/versions/latest/).

## Quick Start

### 1. Setup

```bash
# 1. Fork the repo on GitHub
# 2. Clone your fork
git clone git@github.com:<your-username>/optimum-gateway.git
cd optimum-gateway

# 3. Add the official repo as 'upstream'
git remote add upstream git@github.com:getoptimum/optimum-gateway.git
```

```bash
make build              # Verify the gateway compiles
make run-rlnc-server    # Keep the required RLNC server running in this terminal
```

In a separate terminal:

```bash
make test    # Run tests with the RLNC server available
make run     # Run the service locally
```

## How to Contribute

Here are some ways you can contribute:

* **Open a new issue** (please check the issue does not already exist).
* **Work on an existing issue** (check out the [issue list](https://github.com/getoptimum/optimum-gateway/issues)).

## Submitting a Pull Request

1. **Synchronize** your fork with the latest `upstream/main`.
2. **Create a branch** for your work:
   * Features: `feat/<description>`
   * Bug fixes: `fix/<description>`
   * Documentation: `docs/<description>`
3. **Commit your changes** following [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/).
4. **Push** to your fork and **Open a PR** against `getoptimum/optimum-gateway:main`.

### PR Checklist

Before submitting, ensure:

* [ ] `make run-rlnc-server` is running before tests
* [ ] `make test` passes (including coverage)
* [ ] `make lint` passes (we use `golangci-lint`)
* [ ] Your code is formatted using `go fmt`
* [ ] You've explained **what** changed and **why** in the PR description

## Standards

* **Error Handling**: All errors must be checked.
* **Logging**: Use the internal logger, not `logrus`.
* **Imports**: Use the standard `errors` package, avoid `github.com/pkg/errors`.
* **Commit Messages**: Follow [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/) (e.g., `feat:`, `fix:`, `docs:`).
* **Generated Code**: If you modify `.proto` files, run `make proto` before committing.

## CI Checks

All Pull Requests must pass our CI pipeline (Linting, Tests, Building) before they can be merged. If CI fails, please check the logs and fix the reported issues.

## Questions?

Open a [GitHub Issue](https://github.com/getoptimum/optimum-gateway/issues).
