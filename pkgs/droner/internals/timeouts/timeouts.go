package timeouts

import "time"

const (
	Probe          = 300 * time.Millisecond
	PollInterval   = 3 * time.Second
	SecondShort    = 2 * time.Second
	SecondDefault  = 10 * time.Second
	SecondLong     = 30 * time.Second
	MinutesShort   = 2 * time.Minute
	MinutesDefault = 10 * time.Minute
	MinutesLong    = 30 * time.Minute
)
