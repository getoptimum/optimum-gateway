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
make build   # Verify it compiles
make test    # Run unit tests
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

All pull requests must pass CI before merge.

**Pull requests from a fork** (the usual open-source flow):

1. **Automatic:** secret scanning (TruffleHog) runs without maintainer action.
2. **Skipped on fork `pull_request`:** lint, tests, and other jobs that need private `github.com/getoptimum/*` modules — GitHub does not pass repository secrets to fork PR workflows.
3. **After maintainer review:** a team member adds the **`ok-to-test`** label, which triggers the **Fork CI (ok-to-test)** workflow (lint, tests, coverage) on the PR head commit. Pushing new commits requires re-applying the label.

Run `make lint` and `make test` locally before opening a PR when you can.

## Questions?

Open a [GitHub Issue](https://github.com/getoptimum/optimum-gateway/issues).
