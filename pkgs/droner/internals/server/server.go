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
		slog.Int("port", config.PORT),
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
		s.Logger.Info("starting server")
		err := s.Start()
		if err != nil {
			log.Fatal("[Droner] Failed to start server: " + err.Error())
		}
	}()

	if s.IsRunning() {
		return nil
	}

	return errors.New("Couldn't start server")
}

func (s *Server) IsRunning() bool {
	time.Sleep(2 * time.Second)
	client := &http.Client{Timeout: 200 * time.Second}
	ourVersion := conf.GetConfig().VERSION
	isRunning := false
	for i := range 5 {
		time.Sleep(time.Duration(i) * time.Second)
		s.Logger.Info("Checking server is running", "attempt", i)
		resp, err := client.Get(s.Config.BASE_URL + "/version")
		if err != nil {
			continue
		}

		if resp.StatusCode != http.StatusOK {
			continue
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			continue
		}
		serverVersion := strings.TrimSpace(string(body))
		s.Logger.Info("server version", slog.String("server", serverVersion), slog.String("our", ourVersion))
		resp.Body.Close()
		isRunning = serverVersion == ourVersion
		if isRunning {
			break
		}
	}

	return isRunning
}

func (s *Server) Start() error {
	listener, err := net.Listen("tcp", s.Config.LISTEN_ADDR)
	if err != nil {
		return err
	}
	server := &http.Server{
		Handler: s.Router(),
	}
	return server.Serve(listener)
}
