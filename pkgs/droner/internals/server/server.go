package server

import (
	"droner/conf"
	"droner/internals/logbuf"
	"errors"
	"io"
	"log"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

type Server struct {
	Config *conf.Config
	Env    *conf.EnvStruct
	Logger *slog.Logger
	Logbuf *logbuf.Logger
}

func New() *Server {
	config := conf.GetConfig()
	env := conf.GetEnv()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	buffer := logbuf.New(
		slog.String("version", config.VERSION),
		slog.String("base_path", config.BASE_PATH),
	)
	return &Server{
		Config: config,
		Env:    env,
		Logger: logger,
		Logbuf: buffer,
	}
}

func (s *Server) SafeStart() error {
	if s.IsRunning() {
		return nil
	}

	go func() {
		err := s.Start()
		if err != nil {
			log.Fatal("[Droner] Failed to start server", err)
		}
	}()

	if s.IsRunning() {
		return nil
	}

	return errors.New("Couldn't start server")
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

func (s *Server) Start() error {
	listener, err := net.Listen("tcp", s.Config.BASE_PATH)
	if err != nil {
		return err
	}
	server := &http.Server{
		Handler: s.Router(),
	}
	return server.Serve(listener)
}
