package cliutil

import (
	"fmt"
	"strings"

	"github.com/Oudwins/droner/pkgs/droner/internals/schemas"
)

func PrintSessionCreated(response *schemas.SessionCreateResponse) {
	if harness := strings.TrimSpace(response.Harness.String()); harness != "" {
		fmt.Printf("harness: %s\n", harness)
	}
	branch := ""
	if response.Branch != nil {
		branch = strings.TrimSpace(response.Branch.String())
	}
	if branch != "" {
		fmt.Printf("branch: %s\n", branch)
	}
	if id := strings.TrimSpace(response.ID); id != "" {
		fmt.Printf("id: %s\n", id)
	}
}
