# 


## Installation

```bash
GOPROXY=direct go install github.com/Oudwins/droner/pkgs/droner/droner@latest
GOPROXY=direct go install github.com/Oudwins/droner/pkgs/droner/dronerd@latest

```


## Useful commands


```bash
just cli *args
```

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
  -d '{"path":"/home/tmx/projects/droner/","agentConfig":{"model":"openai/gpt-5.2-codex","prompt":"review the repo"}}'
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



## Hacking opencode

Maybe I can create a / command that integrates with droner. This is what a message with a file reference looks like in opencode:


```json
{"agent":"build","model":{"modelID":"gpt-5.2-codex","providerID":"openai"},"messageID":"msg_bf54f682e002qj4jEFu7OzV2gY","parts":[{"id":"prt_bf54f6830001V0UmZGsImtFIJi","type":"text","text":"Can you see this file @pkgs/droner/droner/cli.go "},{"id":"prt_bf54f682e001K661U5HMKP7X4O","type":"file","mime":"text/plain","url":"file:///home/tmx/projects/droner/pkgs/droner/droner/cli.go","filename":"cli.go","source":{"type":"file","text":{"value":"@pkgs/droner/droner/cli.go","start":22,"end":48},"path":"/home/tmx/projects/droner/pkgs/droner/droner/cli.go"}}]}
```

I would probably need to update any reference file paths to the new file path, assuming I can do that. Then create the session programmatically and attach to it in opencode I open
