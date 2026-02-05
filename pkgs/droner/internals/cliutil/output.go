package cliutil

import (
	"fmt"
	"strings"

	"github.com/Oudwins/droner/pkgs/droner/internals/schemas"
)

func PrintSessionCreated(response *schemas.SessionCreateResponse) {
	simpleID := strings.TrimSpace(response.SimpleID)
	if simpleID == "" && response.SessionID != "" {
		simpleID = response.SessionID.String()
	}
	if simpleID != "" {
		fmt.Printf("session: %s\n", simpleID)
	}
}
