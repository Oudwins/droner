package server

import (
	"droner/conf"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"time"
)

type Server struct {
	Config *conf.Config
	Env    *conf.EnvStruct
}

func New() *Server {
	return &Server{
		Config: conf.GetConfig(),
		Env:    conf.GetEnv(),
	}
}

func (s *Server) Start() error {
	if s.IsRunning() {
		return nil
	}

	go func() {
		err := s.Run()
		if err != nil {
			log.Fatal("[Droner] Failed to start server", err)
		}
	}()

	return nil // TODO:
}

func (s *Server) IsRunning() bool {
	client := &http.Client{Timeout: 200 * time.Millisecond}
	resp, err := client.Get(s.Config.BASE_PATH + "/version")
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return false
	}

	return strings.TrimSpace(string(body)) == s.Config.VERSION
}

func (s *Server) Run() error {
	listener, err := net.Listen("tcp", s.Config.BASE_PATH)
	if err != nil {
		return err
	}
	server := &http.Server{
		Handler: s.Router(),
	}
	return server.Serve(listener)
}
