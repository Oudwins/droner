package assert

func Assert(condition bool, msg string, other ...any) {
	if condition {
		panic(msg)
	}
}

func AssertNil(value any, msg string, other ...any) {
	Assert(value == nil, msg, other...)
}
