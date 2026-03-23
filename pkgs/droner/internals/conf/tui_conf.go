package conf

import (
	"strings"

	z "github.com/Oudwins/zog"
)

var defaultTUIAgentNames = []string{"build", "plan"}

type TUIConfig struct {
	AgentNames []string `json:"agentNames" zog:"agentNames"`
}

var TUIConfigSchema = z.Struct(z.Shape{
	"AgentNames": z.Slice(z.String()).Default(defaultTUIAgentNames).Transform(normalizeAgentNamesTransform),
})

func normalizeAgentNamesTransform(data any, c z.Ctx) error {
	agentNames, ok := data.(*[]string)
	if !ok {
		return nil
	}

	normalized := make([]string, 0, len(*agentNames))
	for _, agentName := range *agentNames {
		agentName = strings.TrimSpace(agentName)
		if agentName == "" {
			continue
		}
		normalized = append(normalized, agentName)
	}
	if len(normalized) == 0 {
		normalized = append(normalized, defaultTUIAgentNames...)
	}
	*agentNames = normalized
	return nil
}
