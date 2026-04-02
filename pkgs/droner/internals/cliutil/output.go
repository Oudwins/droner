package cliutil

import (
	"fmt"
	"strings"

	"github.com/Oudwins/droner/pkgs/droner/internals/schemas"
)

func PrintSessionCreated(response *schemas.SessionCreateResponse) {
	branch := strings.TrimSpace(response.Branch.String())
	if branch != "" {
		fmt.Printf("branch: %s\n", branch)
	}
	if id := strings.TrimSpace(response.ID); id != "" {
		fmt.Printf("id: %s\n", id)
	}
}
