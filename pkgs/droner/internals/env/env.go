package env

import (
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	z "github.com/Oudwins/zog"
	"github.com/Oudwins/zog/zenv"
)

type LogOutput string

type LogLevel string

const (
	LogOutputStd  LogOutput = "std"
	LogOutputFile LogOutput = "file"
	LogOutputBoth LogOutput = "both"

	LogLevelDebug LogLevel = "debug"
	LogLevelInfo  LogLevel = "info"
	LogLevelWarn  LogLevel = "warn"
	LogLevelError LogLevel = "error"
)

func (l LogLevel) SlogLevel() slog.Level {
	switch l {
	case LogLevelInfo:
		return slog.LevelInfo
	case LogLevelWarn:
		return slog.LevelWarn
	case LogLevelError:
		return slog.LevelError
	default:
		return slog.LevelDebug
	}
}

type EnvStruct struct {
	HOME         string    `zog:"HOME"`
	XDG_HOME     string    `zog:"XDG_HOME"`
	PORT         int       `zog:"DRONER_ENV_PORT"`
	GITHUB_TOKEN string    `zog:"GITHUB_TOKEN"`
	LOG_LEVEL    LogLevel  `zog:"DRONERD_LOG_LEVEL"`
	LOG_OUTPUT   LogOutput `zog:"DRONERD_LOG_OUTPUT"`
	DATA_DIR     string    `zog:"DRONERD_DATA_DIR"`
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
	"DATA_DIR":     z.String().Default("~/.droner").Transform(expandPathTransform),
	"LOG_LEVEL":    z.StringLike[LogLevel]().OneOf([]LogLevel{LogLevelDebug, LogLevelInfo, LogLevelWarn, LogLevelError}).Default(LogLevelDebug),
	"LOG_OUTPUT":   z.StringLike[LogOutput]().OneOf([]LogOutput{LogOutputStd, LogOutputFile, LogOutputBoth}).Default(LogOutputFile),
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

func expandPathTransform(ptr *string, c z.Ctx) error {
	expanded, err := expandPath(*ptr)
	*ptr = expanded
	return err
}

func expandPath(path string) (string, error) {
	if path == "" {
		return path, nil
	}
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		if path == "~" {
			return home, nil
		}
		return filepath.Join(home, strings.TrimPrefix(path, "~/")), nil
	}
	return path, nil
}
