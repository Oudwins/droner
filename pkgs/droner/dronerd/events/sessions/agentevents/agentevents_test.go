package agentevents

import "testing"

func TestParseOpenCodeEventSessionStatus(t *testing.T) {
	evt, ok, err := parseOpenCodeEvent(`{"directory":"/tmp/repo..branch","payload":{"type":"session.status","properties":{"sessionID":"abc","status":{"type":"busy"}}}}`)
	if err != nil {
		t.Fatalf("parseOpenCodeEvent: %v", err)
	}
	if !ok {
		t.Fatal("expected session.status event to be recognized")
	}
	if evt.WorktreePath != "/tmp/repo..branch" {
		t.Fatalf("worktree path = %q, want %q", evt.WorktreePath, "/tmp/repo..branch")
	}
	if evt.State != StateBusy {
		t.Fatalf("state = %s, want %s", evt.State, StateBusy)
	}
}

func TestParseOpenCodeEventDeprecatedIdle(t *testing.T) {
	evt, ok, err := parseOpenCodeEvent(`{"directory":"/tmp/repo..branch","payload":{"type":"session.idle","properties":{"sessionID":"abc"}}}`)
	if err != nil {
		t.Fatalf("parseOpenCodeEvent: %v", err)
	}
	if !ok {
		t.Fatal("expected session.idle event to be recognized")
	}
	if evt.State != StateIdle {
		t.Fatalf("state = %s, want %s", evt.State, StateIdle)
	}
}

func TestParseOpenCodeEventIgnoresRetryAndGlobalEvents(t *testing.T) {
	if _, ok, err := parseOpenCodeEvent(`{"directory":"global","payload":{"type":"server.connected","properties":{}}}`); err != nil || ok {
		t.Fatalf("global event => ok=%v err=%v, want ok=false err=nil", ok, err)
	}
	if _, ok, err := parseOpenCodeEvent(`{"directory":"/tmp/repo..branch","payload":{"type":"session.status","properties":{"sessionID":"abc","status":{"type":"retry","attempt":1,"message":"retrying","next":123}}}}`); err != nil || ok {
		t.Fatalf("retry event => ok=%v err=%v, want ok=false err=nil", ok, err)
	}
}
