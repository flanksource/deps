---
cwd: $GIT_ROOT_DIR
---

# Service aliases and Silo

| Name | Command | Exit Code | CEL Validation |
|------|---------|-----------|----------------|
| Builds | make build build-start | 0 | exitCode == 0 |
| Root regression tests | go test ./cmd ./pkg/config ./pkg/utils ./pkg/version ./pkg/types ./pkg/installer ./pkg/lock ./pkg/extract | 0 | exitCode == 0 |
| Service unit tests | go -C start test ./... -ginkgo.label-filter='!integration' | 0 | exitCode == 0 |
| Silo live lifecycle and chart | go -C start test . -ginkgo.focus='Silo live' -ginkgo.label-filter=integration -timeout=10m | 0 | exitCode == 0 |
| Start alias help | PATH="$PWD/.bin:$PATH" .bin/deps start silo --help | 0 | stdout.contains("binary, docker, helm") |
| Services alias help | PATH="$PWD/.bin:$PATH" .bin/deps services silo --help | 0 | stdout.contains("--region") |
| Child error propagation | PATH="$PWD/.bin:$PATH" .bin/deps services unknown-service | 1 | stderr.contains("unknown") |
| Lint | make lint lint-start | 0 | exitCode == 0 |
