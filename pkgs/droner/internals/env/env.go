package env

import (
	"log"
	"strconv"

	z "github.com/Oudwins/zog"
	"github.com/Oudwins/zog/zenv"
)

type EnvStruct struct {
	HOME         string `zog:"HOME"`
	XDG_HOME     string `zog:"XDG_HOME"`
	PORT         int    `zog:"DRONER_ENV_PORT"`
	GITHUB_TOKEN string `zog:"GITHUB_TOKEN"`
	LISTEN_ADDR  string
	LISTEN_PROT  string
	BASE_URL     string
}

var env *EnvStruct

var EnvSchema = z.Struct(z.Shape{
	"HOME":         z.String(),
	"XDG_HOME":     z.String(),
	"PORT":         z.Int().Default(57876),
	"GITHUB_TOKEN": z.String().Optional(),
})

func Get() *EnvStruct {
	if env == nil {
		env = &EnvStruct{}
		errs := EnvSchema.Parse(zenv.NewDataProvider(), env)
		if errs != nil {
			log.Fatal("[Droner] Failed to parse environment variables", errs)
		}

		env.LISTEN_PROT = "http://"
		env.LISTEN_ADDR = "localhost:" + strconv.Itoa(env.PORT)
		env.BASE_URL = env.LISTEN_PROT + env.LISTEN_ADDR

	}
	return env
}
