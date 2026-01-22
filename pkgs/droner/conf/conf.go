package conf

import (
	"log"
	"log/slog"
	"strconv"

	z "github.com/Oudwins/zog"
	"github.com/Oudwins/zog/zenv"
)

type EnvStruct struct {
	HOME     string `zog:"$HOME"`
	XDG_HOME string `zog:"$XDG_HOME"`
}

var env *EnvStruct

var EnvSchema = z.Struct(z.Shape{
	"HOME":     z.String(),
	"XDG_HOME": z.String(),
})

func GetEnv() *EnvStruct {
	if env == nil {
		env = &EnvStruct{}
		errs := EnvSchema.Parse(zenv.NewDataProvider(), env)
		if errs != nil {
			log.Fatal("[Droner] Failed to parse environment variables", errs)
		}
	}
	return env
}

type Config struct {
	VERSION      string
	PORT         int
	LISTEN_ADDR  string
	LISTEN_PROT  string
	BASE_URL     string
	WORKTREE_DIR string
}

var ConfigSchema = z.Struct(z.Shape{
	"PORT":         z.Int().Default(57876),
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
		config.LISTEN_PROT = "http://"
		config.LISTEN_ADDR = "localhost:" + strconv.Itoa(config.PORT)
		config.BASE_URL = config.LISTEN_PROT + config.LISTEN_ADDR
		slog.Info("config", slog.String("version", config.VERSION), slog.String("base_path", config.LISTEN_ADDR))
	}

	return config
}
