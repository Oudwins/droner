package assert

import "fmt"

func Assert(condition bool, msg string, other ...any) {
	if condition {
		panic(msg)
	}
}

func AssertNil(value any, msg string, other ...any) {
	if value == nil {
		return
	}
	panic(fmt.Sprintf("%s:%v:%v", msg, value, other))
}
