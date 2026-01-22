package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"droner/conf"
	"droner/internals/schemas"
)

var ErrUsage = errors.New("usage: droner sum <a> <b>")

func Run(args []string) error {
	if len(args) == 0 {
		return ErrUsage
	}

	if args[0] != "sum" {
		return ErrUsage
	}

	if len(args) != 3 {
		return ErrUsage
	}

	if _, err := strconv.Atoi(args[1]); err != nil {
		return ErrUsage
	}
	if _, err := strconv.Atoi(args[2]); err != nil {
		return ErrUsage
	}

	config := conf.GetConfig()

	endpoint := fmt.Sprintf("%s/sum?a=%s&b=%s", config.BASE_URL, args[1], args[2])
	resp, err := http.Get(endpoint)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server error: %s", resp.Status)
	}

	var payload schemas.SumResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return err
	}

	fmt.Println(payload.Sum)
	return nil
}
