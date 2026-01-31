package assert

func Assert(condition bool, msg string, other ...any) {
	if condition {
		panic(msg)
	}
}

func AssertNil(condition any, msg string, other ...any) {
	Assert(condition == nil, msg, other...)
}
