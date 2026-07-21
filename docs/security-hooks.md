# Pre-Commit Hook Security Setup

## Overview

This document explains the automated security pre-commit hook system that prevents accidental commits of secrets, credentials, and sensitive data to git repositories.

Pre-commit hooks are automated scripts that run **before** a git commit is finalized. If issues are detected (like secrets in your code), the commit is blocked, preventing sensitive data from entering the repository history.

## Security Checks Enabled

Our pre-commit configuration includes three layers of protection:

1. **Gitleaks (v8.30.0)** - Detects AWS keys, API tokens, passwords, and 100+ secret patterns
2. **Detect Private Key** - Catches RSA, DSA, EC, and other private key formats
3. **Check Merge Conflicts** - Detects merge conflict markers to ensure conflicts are resolved

## Setup

### Global Configuration (One-Time)

```bash
# Install pre-commit
brew install pre-commit

# Create template directory and configure git
mkdir -p ~/.git-templates/hooks
git config --global init.templateDir ~/.git-templates

# Install pre-commit hook in template
cd ~/.git-templates
pre-commit init-templatedir .
```

This automatically installs hooks in every repository you clone or initialize.

### Repository Configuration

The repository already includes:
- `.pre-commit-config.yaml` - Defines which security checks to run
- `.gitleaks.toml` - Configures secret detection rules and allowlists

## Usage

### Normal Workflow

Hooks run automatically on every commit:

```bash
git add config.go
git commit -m "Update configuration"
```

If secrets are detected, the commit is blocked:

```
Detect secrets with Gitleaks.............................................Failed
Finding:     AWS_SECRET_KEY="AKIAIOSFODNN7EXAMPLE"
File:        config.go
Line:        42
```

### Bypassing Hooks (Emergency Only)

⚠️ **Only use when absolutely necessary:**

```bash
git commit --no-verify -m "Emergency commit"
```

**Never bypass when:**
- Committing to main/master branch
- Creating pull requests
- Working with production code

## Common Commands

```bash
# Run hooks manually on all files
pre-commit run --all-files

# Run specific hook
pre-commit run gitleaks --all-files

# Update hooks to latest versions
pre-commit autoupdate

# Reinstall hooks if missing
pre-commit install
```

## Troubleshooting

### Hook not running

```bash
# Check if installed
ls -la .git/hooks/pre-commit

# Reinstall
pre-commit install
```

### False Positives

Add patterns to `.gitleaks.toml` allowlist:

```toml
[allowlist]
regexes = [
    '''your-false-positive-pattern''',
]

paths = [
    '''path/to/file\.txt$''',
]
```

### Gitleaks not found

```bash
brew install gitleaks
# Or let pre-commit install it
pre-commit run gitleaks --all-files
```

## What Gets Detected

- AWS/GCP/Azure credentials
- API keys (GitHub, Stripe, Slack, etc.)
- Private keys (RSA, DSA, EC, SSH)
- Database connection strings with passwords
- JWT and OAuth tokens
- Generic high-entropy secrets

## Best Practices

**Do:**
- Always investigate hook failures
- Update hooks monthly: `pre-commit autoupdate`
- Add false positives to allowlist rather than bypassing
- Test hooks work after setup

**Don't:**
- Use `--no-verify` routinely
- Ignore hook failures
- Commit secrets even if caught by hooks

## If a Secret Was Committed

1. **Rotate the credential immediately** - Assume it's compromised
2. **Remove from git history** using `git-filter-repo` or `BFG Repo-Cleaner`
3. **Force push** to update remote (coordinate with team)
4. **Audit access logs** to check if credential was accessed

## Resources

- Pre-commit: https://pre-commit.com/
- Gitleaks: https://github.com/gitleaks/gitleaks
- Git Hooks: https://git-scm.com/docs/githooks

---

**Last Updated:** January 14, 2026
