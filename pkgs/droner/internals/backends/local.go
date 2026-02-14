package backends

import "github.com/Oudwins/droner/pkgs/droner/internals/conf"

type LocalBackend struct {
	config *conf.LocalBackendConfig
}

func (l LocalBackend) ID() conf.BackendID {
	return conf.BackendLocal
}

func RegisterLocal(store *Store, config *conf.LocalBackendConfig) {
	if store == nil {
		return
	}
	store.Register(LocalBackend{config: config})
}
