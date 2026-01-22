package conf

import (
	"log"
	"strconv"

	z "github.com/Oudwins/zog"
	"github.com/Oudwins/zog/zenv"
)

type EnvStruct struct {
	HOME     string `zog:"$HOME"`
	XDG_HOME string `zog:"$XDG_HOME"`
}

var env *EnvStruct = nil

var EnvSchema = z.Struct(z.Shape{
	"HOME":     z.String(),
	"XDG_HOME": z.String(),
})

func GetEnv() *EnvStruct {
	if env == nil {
		errs := EnvSchema.Parse(zenv.NewDataProvider(), &env)
		log.Fatal("[Droner] Failed to parse environment variables", errs)
	}
	return env
}

type Config struct {
	VERSION   string
	PORT      int
	BASE_PATH string
}

var ConfigSchema = z.Struct(z.Shape{
	"PORT":      z.Int().Default(57876),
	"BASE_PATH": z.String(),
	// type Transform[T any] func(valPtr T, ctx Ctx) error
	"VERSION": z.String().Transform(func(valPtr *string, ctx z.Ctx) error {
		*valPtr = "0.0.1" // override whatever is in the config file force this value
		return nil
	}),
}).Transform(func(valPtr any, ctx z.Ctx) error {
	v := valPtr.(*Config)
	v.BASE_PATH = "http://localhost:" + strconv.Itoa(v.PORT)
	return nil
})
var config *Config = nil

func GetConfig() *Config {

	if config == nil {
		// TODO: We need to parse this form config file
		err := ConfigSchema.Parse(map[string]any{}, &config)
		log.Fatal("[Droner] Failed to parse config", err)
	}

	return config
}
