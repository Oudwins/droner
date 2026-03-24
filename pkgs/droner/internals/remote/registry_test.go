package remote

import "sync"

// ResetRegistryForTests clears global state between tests.
func ResetRegistryForTests() {
	if globalRegistry != nil && globalRegistry.provider != nil {
		globalRegistry.provider.close()
	}
	once = sync.Once{}
	globalRegistry = nil
}
