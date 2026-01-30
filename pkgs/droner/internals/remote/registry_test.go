package remote

import "sync"

// ResetRegistryForTests clears global state between tests.
func ResetRegistryForTests() {
	once = sync.Once{}
	globalRegistry = nil
}
