package backends

type LocalBackend struct {
	worktreeRoot string
}

func (l LocalBackend) ID() BackendID {
	return BackendLocal
}

func RegisterLocal(store *Store, worktreeRoot string) {
	if store == nil {
		return
	}
	store.Register(LocalBackend{worktreeRoot: worktreeRoot})
}
