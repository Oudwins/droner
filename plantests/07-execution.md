# Step 7: Execution Checklist

## Local run
- `go test ./pkgs/droner/...`
- Optional: `go test -race ./pkgs/droner/internals/logbuf`

## Determinism checklist
- No real network calls.
- No real tmux or git side effects.
- All temp paths are under `t.TempDir()`.
- Time-based flows use time seam.
- Global state reset between tests.

## Verify
- Tests pass repeatedly without flakes.
