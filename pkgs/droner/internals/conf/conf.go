package conf

import (
	"log"

	z "github.com/Oudwins/zog"
)

type Config struct {
	VERSION      string
	WORKTREE_DIR string
}

var ConfigSchema = z.Struct(z.Shape{
	"WORKTREE_DIR": z.String().Default("~/.droner"),
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
