# Example Scripts for `deps run`

This directory contains example scripts demonstrating the `deps run` command across multiple languages.

## Available Examples

- **hello.py** - Python script
- **hello.js** - JavaScript (Node.js) script
- **hello.ts** - TypeScript script
- **Hello.java** - Java program
- **hello.ps1** - PowerShell script

## Basic Usage

```bash
# Python
deps run examples/scripts/hello.py

# JavaScript
deps run examples/scripts/hello.js

# TypeScript (requires tsx or ts-node)
deps run examples/scripts/hello.ts

# Java (automatically compiles and runs)
deps run examples/scripts/Hello.java

# PowerShell
deps run examples/scripts/hello.ps1
```

## Passing Arguments

Pass arguments after the script path:

```bash
deps run examples/scripts/hello.py arg1 arg2
deps run examples/scripts/hello.js "hello world"
```

## Advanced Usage

### With Version Constraints

```bash
# Require Python 3.9 or higher
deps run --version ">=3.9" examples/scripts/hello.py

# Require Node.js 18 or higher
deps run --version ">=18" examples/scripts/hello.js
```

### With Timeout

```bash
# Set 30 second timeout
deps run --timeout 30s examples/scripts/hello.py
```

### With Environment Variables

```bash
# Pass environment variables
deps run --env "API_KEY=secret" --env "DEBUG=true" examples/scripts/hello.py
```

### With Custom Working Directory

```bash
# Run in specific directory
deps run --working-dir /tmp examples/scripts/hello.py
```

## TypeScript Requirements

TypeScript execution requires either `tsx` or `ts-node`:

```bash
# Install tsx (recommended - faster)
npm install -g tsx

# Or install ts-node
npm install -g ts-node
```

## Output

All scripts output basic information including:
- Language/runtime version
- Platform information
- Any arguments passed to the script

Example output:

```
Hello from Python 3.11.5!
Platform: Darwin arm64
Arguments: arg1 arg2
```
