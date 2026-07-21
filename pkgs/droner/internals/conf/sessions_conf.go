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
	HarnessOpenCode HarnessID = "opencode"
)

type HarnessID string

var configHarnessIDSchema = z.StringLike[HarnessID]().OneOf([]HarnessID{HarnessOpenCode}).Default(HarnessOpenCode)

var HarnessIDSchema = z.StringLike[HarnessID]().OneOf([]HarnessID{HarnessOpenCode}).DefaultFunc(func() HarnessID {
	return GetConfig().Sessions.Harness.Defaults.Selected
})

func (h HarnessID) String() string {
	return string(h)
}

type OpenCodeConfig struct {
	DefaultModel string `json:"defaultModel" zog:"defaultModel"`
	Hostname     string `json:"hostname" zog:"hostname"`
	Port         int    `json:"port" zog:"port"`
}

type SessionNamingStrategy string

const (
	SessionNamingStrategyRandom         SessionNamingStrategy = "random"
	SessionNamingStrategyOpenCodePrompt SessionNamingStrategy = "opencode_prompt"
)

type SessionNamingConfig struct {
	Strategy SessionNamingStrategy `json:"strategy" zog:"strategy"`
	Model    string                `json:"model" zog:"model"`
}

type LocalBackendConfig struct {
	WorktreeDir string `json:"worktreeDir" zog:"worktreeDir"`
}

type SessionHarnessProvidersConfig struct {
	OpenCode OpenCodeConfig `json:"openCode" zog:"openCode"`
}

type SessionHarnessDefaultsConfig struct {
	Selected HarnessID `json:"selected" zog:"selected"`
}

type SessionHarnessConfig struct {
	Defaults  SessionHarnessDefaultsConfig  `json:"defaults" zog:"defaults"`
	Providers SessionHarnessProvidersConfig `json:"providers" zog:"providers"`
}

func (c SessionHarnessConfig) DefaultModel() string {
	switch c.Defaults.Selected {
	case "", HarnessOpenCode:
		return c.Providers.OpenCode.DefaultModel
	default:
		return ""
	}
}

type BackendsConfig struct {
	Default BackendID          `json:"default" zog:"default"`
	Local   LocalBackendConfig `json:"local" zog:"local"`
}

type SessionsConfig struct {
	Backends BackendsConfig       `json:"backends" zog:"backends"`
	Harness  SessionHarnessConfig `json:"harness" zog:"harness"`
	Naming   SessionNamingConfig  `json:"naming" zog:"naming"`
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
	"Harness": z.Struct(z.Shape{
		"Defaults": z.Struct(z.Shape{
			"Selected": configHarnessIDSchema,
		}),
		"Providers": z.Struct(z.Shape{
			"OpenCode": z.Struct(z.Shape{
				"DefaultModel": z.String().Default("openai/gpt-5.5").Trim(),
				"Hostname":     z.String().Default("127.0.0.1"),
				"Port":         z.Int().Default(4096),
			}),
		}),
	}),
	"Naming": z.Struct(z.Shape{
		"Strategy": z.StringLike[SessionNamingStrategy]().OneOf([]SessionNamingStrategy{SessionNamingStrategyRandom, SessionNamingStrategyOpenCodePrompt}).Default(SessionNamingStrategyOpenCodePrompt),
		"Model":    z.String().Default("openai/gpt-5-mini").Trim(),
	}),
})
