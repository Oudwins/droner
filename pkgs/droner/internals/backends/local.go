package backends

import (
	"github.com/Oudwins/droner/pkgs/droner/dronerd/core/db"
	"github.com/Oudwins/droner/pkgs/droner/internals/conf"
)

type LocalBackend struct {
	config *conf.LocalBackendConfig
	db     *db.Queries
}

func (l LocalBackend) ID() conf.BackendID {
	return conf.BackendLocal
}

func RegisterLocal(store *Store, config *conf.LocalBackendConfig, queries *db.Queries) {
	if store == nil {
		return
	}
	store.Register(LocalBackend{config: config, db: queries})
}
