package server

import (
	"errors"
	"io"
	"log"
	"log/slog"
	"math"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Oudwins/droner/pkgs/droner/dronerd/tasks"
	"github.com/Oudwins/droner/pkgs/droner/internals/conf"
	"github.com/Oudwins/droner/pkgs/droner/internals/env"
	"github.com/Oudwins/droner/pkgs/droner/internals/logbuf"
	"github.com/Oudwins/droner/pkgs/droner/internals/tasky"
)

type Server struct {
	Config     *conf.Config
	Env        *env.EnvStruct
	Logger     *slog.Logger
	Logbuf     *logbuf.Logger
	subs       *subscriptionManager
	oauth      *oauthStateStore
	tasks      *taskManager
	httpServer *http.Server
	tasky      *tasky.Queue[tasks.Jobs]
}

func New() *Server {
	config := conf.GetConfig()
	env := env.Get()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	buffer := logbuf.New(
		slog.String("version", config.Version),
		slog.Int("port", env.PORT),
	)
	worktreeRoot, err := expandPath(config.Worktrees.Dir)
	if err != nil {
		log.Fatal("[Droner] Failed to expand worktree root: ", err)
	}
	dataDir := filepath.Join(worktreeRoot, ".droner")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		log.Fatal("[Droner] Failed to create task data dir: ", err)
	}
	dbPath := filepath.Join(dataDir, "tasks.db")
	store, err := newTaskStore(dbPath)
	if err != nil {
		log.Fatal("[Droner] Failed to initialize task store: ", err)
	}
	manager := newTaskManager(store, logger)
	return &Server{
		Config: config,
		Env:    env,
		Logger: logger,
		Logbuf: buffer,
		subs:   newSubscriptionManager(),
		oauth:  newOAuthStateStore(),
		tasks:  manager,
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
	resp, err := client.Get(s.Env.BASE_URL + "/version")
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
	listener, err := net.Listen("tcp", s.Env.LISTEN_ADDR)
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
