package conf

import z "github.com/Oudwins/zog"

const (
	BackendLocal BackendID = "local"
)

type BackendID string

// TODO. Handle default value here
var BackendIDSchema = z.StringLike[BackendID]().OneOf([]BackendID{BackendLocal}).DefaultFunc(func() BackendID {
	return BackendLocal
})

func (b BackendID) String() string {
	return string(b)
}

const (
	AgentProviderOpenCode AgentProviderID = "opencode"
)

type AgentProviderID string

var AgentProviderIDSchema = z.StringLike[AgentProviderID]().OneOf([]AgentProviderID{AgentProviderOpenCode})

func (b AgentProviderID) String() string {
	return string(b)
}

type OpenCodeConfig struct {
	Hostname string
	Port     int
}

type LocalBackendConfig struct {
	WorktreeDir string
}

type AgentProvidersConfig struct {
	OpenCode OpenCodeConfig
}

type AgentConfig struct {
	DefaultProvider AgentProviderID
	DefaultModel    string
	Providers       AgentProvidersConfig
}

type BackendsConfig struct {
	Default BackendID
	Local   LocalBackendConfig
}

type SessionsConfig struct {
	Backends BackendsConfig
	Agent    AgentConfig
}

var SessionsConfigSchema = z.Struct(z.Shape{
	"Backends": z.Struct(
		z.Shape{
			"Default": BackendIDSchema,
			"Local": z.Struct(z.Shape{
				"WorktreeDir": z.String().Default("~/.droner/worktrees").Transform(expandPathTransform),
			}),
		},
	),
	"Agent": z.Struct(z.Shape{
		"DefaultProvider": AgentProviderIDSchema,
		"DefaultModel":    z.String(),
		"Providers": z.Struct(z.Shape{
			"OpenCode": z.Struct(z.Shape{
				"Hostname": z.String().Default("127.0.0.1"),
				"Port":     z.Int().Default(4096),
			}),
		}),
	}),
})
