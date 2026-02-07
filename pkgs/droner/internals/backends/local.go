package backends

type LocalBackend struct{}

func (LocalBackend) ID() BackendID {
	return BackendLocal
}

func RegisterLocal(store *Store) {
	if store == nil {
		return
	}
	store.Register(LocalBackend{})
}
