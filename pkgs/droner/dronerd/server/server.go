package server

import (
	"errors"
	"io"
	"log"
	"log/slog"
	"math"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/Oudwins/droner/pkgs/droner/dronerd/baseserver"
	"github.com/Oudwins/droner/pkgs/droner/dronerd/tasks"
	"github.com/Oudwins/droner/pkgs/droner/internals/assert"
	"github.com/Oudwins/droner/pkgs/droner/internals/logbuf"
	"github.com/Oudwins/droner/pkgs/droner/internals/tasky"
)

type Server struct {
	Base       *baseserver.BaseServer
	Logbuf     *logbuf.Logger
	subs       *subscriptionManager
	oauth      *oauthStateStore
	tasks      *taskManager
	httpServer *http.Server
	tasky      *tasky.Queue[tasks.Jobs]
}

func New() *Server {
	base := baseserver.New()
	buffer := logbuf.New(
		slog.String("version", base.Config.Version),
		slog.Int("port", base.Env.PORT),
	)

	q, error := tasks.NewQueue(base)
	assert.AssertNil(error, "[SERVER] Failed to initialize")
	return &Server{
		Base:   base,
		Logbuf: buffer,
		tasky:  q,
	}
}

func (s *Server) SafeStart() error {
	if s.IsRunning() {
		return nil
	}

	go func() {
		s.Base.Logger.Info("starting server")
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
		s.Base.Logger.Info("Waiting for server to start", "attempt", i)
		if isRunning := s.IsRunning(); isRunning {
			return true
		}
		time.Sleep(time.Duration(math.Pow(2, float64(i))) * time.Second)
	}

	return false
}

func (s *Server) IsRunning() bool {
	client := &http.Client{Timeout: 200 * time.Millisecond}
	resp, err := client.Get(s.Base.Env.BASE_URL + "/version")
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
	_ = strings.TrimSpace(string(body))
	resp.Body.Close()
	return true
}

func (s *Server) Start() error {
	listener, err := net.Listen("tcp", s.Base.Env.LISTEN_ADDR)
	if err != nil {
		return err
	}
	server := &http.Server{
		Handler: s.Router(),
	}
	s.httpServer = server
	err = server.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
