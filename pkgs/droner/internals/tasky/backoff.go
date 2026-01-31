package tasky

import (
	"math"
	"time"
)

type BackoffConfig struct {
	Base   time.Duration
	Max    time.Duration
	Factor float64
}

func BackoffExponential(cfg BackoffConfig) func(attempts int) time.Duration {
	base := cfg.Base
	max := cfg.Max
	factor := cfg.Factor
	if factor <= 0 {
		factor = 2
	}

	return func(attempts int) time.Duration {
		if attempts <= 0 || base <= 0 {
			return 0
		}
		exponent := float64(attempts - 1)
		delay := float64(base) * math.Pow(factor, exponent)
		if delay < 0 {
			return 0
		}
		if max > 0 && delay > float64(max) {
			return max
		}
		if delay > float64(math.MaxInt64) {
			if max > 0 {
				return max
			}
			return time.Duration(math.MaxInt64)
		}
		return time.Duration(delay)
	}
}
