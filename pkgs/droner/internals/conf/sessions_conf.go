package conf

import z "github.com/Oudwins/zog"

const (
	BackendLocal BackendID = "local"
)

type BackendID string

var configBackendIDSchema = z.StringLike[BackendID]().OneOf([]BackendID{BackendLocal}).Default(BackendLocal)

// Copy of above but that uses the config to set default
var BackendIDSchema = z.StringLike[BackendID]().OneOf([]BackendID{BackendLocal}).DefaultFunc(func() BackendID {
	return GetConfig().Sessions.Backends.Default
	// return BackendLocal
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

type SessionNamingStrategy string

const (
	SessionNamingStrategyRandom         SessionNamingStrategy = "random"
	SessionNamingStrategyOpenCodePrompt SessionNamingStrategy = "opencode_prompt"
)

type SessionNamingConfig struct {
	Strategy SessionNamingStrategy
	Model    string
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
	Naming   SessionNamingConfig
}

var SessionsConfigSchema = z.Struct(z.Shape{
	"Backends": z.Struct(
		z.Shape{
			"Default": configBackendIDSchema,
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
	"Naming": z.Struct(z.Shape{
		"Strategy": z.StringLike[SessionNamingStrategy]().OneOf([]SessionNamingStrategy{SessionNamingStrategyRandom, SessionNamingStrategyOpenCodePrompt}).Default(SessionNamingStrategyOpenCodePrompt),
		"Model":    z.String().Default("openai/gpt-5-mini").Trim(),
	}),
})
