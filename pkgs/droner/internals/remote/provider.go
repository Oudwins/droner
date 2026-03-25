package remote

// remoteProvider abstracts provider-specific implementations.
type remoteProvider interface {
	isValidKey(key subscriptionKey) bool
	subscribe(key subscriptionKey)
	unsubscribe(key subscriptionKey)
	close()
	ensureAuth() error
}
