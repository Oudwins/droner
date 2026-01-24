# 


## Installation

```bash

go install github.com/Oudwins/droner/pkgs/droner/cli@latest
go install github.com/Oudwins/droner/pkgs/droner/dronerd@latest

```


## Useful commands

```bash
# is server running
curl -X GET http://localhost:57876/version
# Create session (auto session_id):
curl -sS -X POST http://localhost:57876/sessions \
  -H "Content-Type: application/json" \
  -d '{"path":"/home/tmx/projects/droner/"}'
# Create session (explicit session_id):
curl -sS -X POST http://localhost:57876/sessions \
  -H "Content-Type: application/json" \
  -d '{"path":"/home/tmx/projects/droner/","session_id":"abc-12"}'
# Create session with agent prompt/model:
curl -sS -X POST http://localhost:57876/sessions \
  -H "Content-Type: application/json" \
  -d '{"path":"/home/tmx/projects/droner/","agent":{"model":"openai/gpt-5.2-codex","prompt":"review the repo"}}'
# Delete session by path:
curl -sS -X DELETE http://localhost:57876/sessions \
  -H "Content-Type: application/json" \
  -d '{"path":"/home/tmx/projects/droner/"}'
# Delete session by session_id:
curl -sS -X DELETE http://localhost:57876/sessions \
  -H "Content-Type: application/json" \
  -d '{"session_id":"abc-12"}'
```





## Notes
- Maybe refactor the command line utility with: https://github.com/spf13/cobra
