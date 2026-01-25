package conf

import (
	"log"

	z "github.com/Oudwins/zog"
)

type Config struct {
	VERSION       string
	WORKTREES_DIR string
	DATA_DIR      string
	DEFAULT_MODEL string
}

var ConfigSchema = z.Struct(z.Shape{
	"WORKTREES_DIR": z.String().Default("~/.local/share/droner/worktrees"),
	"DATA_DIR":      z.String().Default("~/.local/share/droner"),
	"DEFAULT_MODEL": z.String().Default("openai/gpt-5.2-codex"),
})
var config *Config

func GetConfig() *Config {

	if config == nil {
		// TODO: We need to parse this form config file
		config = &Config{}
		err := ConfigSchema.Parse(map[string]any{}, config)
		if err != nil {
			log.Fatal("[Droner] Failed to parse config", err)
		}
		config.VERSION = "0.0.1"
	}

	return config
}
