package sessionids

import (
	"fmt"
	"math/rand"
	"time"
)

type GeneratorConfig struct {
	MaxAttempts int
	IsValid     func(id string) error
}

func New(baseName string, conf *GeneratorConfig) (string, error) {
	random := rand.New(rand.NewSource(time.Now().UnixNano()))
	letters := []rune("abcdefghijklmnopqrstuvwxyz")
	var err error
	for range conf.MaxAttempts {
		chars := make([]rune, 3)
		for i := range chars {
			chars[i] = letters[random.Intn(len(letters))]
		}
		candidate := fmt.Sprintf("%s-%02d", string(chars), random.Intn(100))

		err = conf.IsValid(candidate)

		if err == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("no available session id: %s", err)
}
