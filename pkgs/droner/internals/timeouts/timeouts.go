package timeouts

import "time"

const (
	Probe          = 300 * time.Millisecond
	PollInterval   = 3 * time.Second
	SecondShort    = 2 * time.Second
	SecondDefault  = 10 * time.Second
	SecondLong     = 30 * time.Second
	DefaultMinutes = 20 * time.Minute
)
