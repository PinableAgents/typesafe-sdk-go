# Working with this Go SDK

Go language baseline: **1.23**. Module: `github.com/PinableAgents/typesafe-sdk-go`.

## Public entry points

- Native SDK: `typesafe.NewClient`, `SystemOne`, `ListModels`, `Models().List`.
- Conservative task policy: `contrib/agentpolicy`.
- Provider-neutral tools: `contrib/agenttool.Definitions`, `New`, `Registry.Call`.
- One-shot JSON CLI: `go run ./cmd/typesafe-tool --list`; read `docs/agent-tools.zh-CN.md`.

The CLI is not an MCP server. The host must register tools or invoke a subprocess.
Never pretend installation alone registers tools in Claude Code, Codex, or another host.

## Credentials and permissions

Keep `TYPESAFE_API_KEY` in host environment variables or a CI secret, never source,
fixtures, prompts, command-line arguments, PR descriptions or logs. Do not send entire
repositories or environment dumps. Tool arguments cannot override API URLs, headers,
keys or `ExtraBody`. Do not add such a bypass to the Agent adapter.

Model outputs are advisory. `ok:false` and `error.requires_review:true` are not safe-to-proceed
results. Check both `ok` and the route's `requires_review`. Even an `explain` result cannot
authorize file writes, commands, network access or bypass host approval/sandbox rules.

## Validation

Run `go test -count=1 ./...`, `go test -race -count=1 ./...`, `go vet ./...` and `go build ./...`.
The validation package includes a separate Go 1.23 consumer module check.
`python3 scripts/validate_typesafe.py` additionally captures evidence; Python is not an SDK
or CLI runtime dependency.

Paid validation requires explicit consent and `TYPESAFE_API_KEY`:
`python3 scripts/validate_typesafe.py --live`. The component suite currently has 29 cases,
28 HTTP requests on the no-retry success path, a cap of 36 transport attempts and maximum
concurrency 2. It gates evaluations on the models preflight. Never mark skipped tests,
mock fixtures, DNS failures, a CI green offline job or cross-compilation as real API success.
Use `-count=1`; preserve commit/toolchain/model/request-count evidence and sanitized logs.

New tool kinds need definitions, runtime validation, structured errors, offline tests and
explicit live-suite coverage. Preserve JSON numbers in structured instructions/criteria.
