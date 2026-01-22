package server

import (
	"droner/conf"
	"droner/internals/logbuf"
	"errors"
	"io"
	"log"
	"log/slog"
	"math"
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

	if s.waitForStart() {
		return nil
	}

	return errors.New("Couldn't start server")
}

func (s *Server) waitForStart() bool {
	time.Sleep(2 * time.Second)
	for i := range 6 {
		s.Logger.Info("Waiting for server to start", "attempt", i)
		if isRunning := s.IsRunning(); isRunning {
			return true
		}
		time.Sleep(time.Duration(math.Pow(2, float64(i))) * time.Second)
	}

	return false
}

func (s *Server) IsRunning() bool {
	client := &http.Client{Timeout: 200 * time.Millisecond}
	ourVersion := conf.GetConfig().VERSION
	resp, err := client.Get(s.Config.BASE_URL + "/version")
	if err != nil {
		return false
	}

	if resp.StatusCode != http.StatusOK {
		return false
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return false
	}
	serverVersion := strings.TrimSpace(string(body))
	s.Logger.Info("server version", slog.String("server", serverVersion), slog.String("our", ourVersion))
	resp.Body.Close()
	return serverVersion == ourVersion
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
