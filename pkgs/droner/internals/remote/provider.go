package remote

// provider interface abstracts provider-specific implementations
type provider interface {
	subscribe(key subscriptionKey)
	unsubscribe(key subscriptionKey)
	close()
	ensureAuth() error
}
