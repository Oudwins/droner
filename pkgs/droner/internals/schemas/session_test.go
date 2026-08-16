package schemas

import "testing"

func TestNewSBranchPreservesBranchString(t *testing.T) {
	branch := NewSBranch("feature/foo.bar")
	if branch.String() != "feature/foo.bar" {
		t.Fatalf("branch = %q, want feature/foo.bar", branch.String())
	}
}

func TestSessionCreateSchemaAcceptsSlashBranch(t *testing.T) {
	var request SessionCreateRequest
	if err := SessionCreateSchema.Parse(SessionCreateRequest{Path: "/tmp/repo", Branch: SBranch("feature/foo")}, &request); err != nil {
		t.Fatalf("SessionCreateSchema.Parse returned error: %v", err)
	}

}
