package

import (
	"context"
	"github.com/arriqaaq/x"
	"log"
	"net"
	"net/http"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

const (
	schemeHTTP = "http"
)

var defaultSchemes []string

func init() {
	defaultSchemes = []string{
		schemeHTTP,
	}
}

// NewServer creates a new api server but does not configure it
func NewServer(handler http.Handler) *Server {
	s := new(Server)
	s.shutdown = make(chan struct{})
	s.doneCh = make(chan struct{})
	s.SetHandler(handler)
	return s
}

// Server for the auction API
type Server struct {
	EnabledListeners []string      //the listeners to enable
	CleanupTimeout   time.Duration //grace period for which to wait before shutting down the server
	MaxHeaderSize    int           //controls the maximum number of bytes the server will read parsing the request header's keys and values, including the request line

	SocketPath    os.Fil //the unix socket to listen on" default:"/var/run/server.sock"
	domainSocketL net.Listener

	Host         string        //the IP to listen on" default:"localhost" env:"HOST"
	Port         int           //the port to listen on for insecure connections, defaults to a random value" env:"PORT"
	ListenLimit  int           //limit the number of outstanding requests"
	KeepAlive    time.Duration //sets the TCP keep-alive timeouts on accepted connections. It prunes dead TCP connections
	ReadTimeout  time.Duration //maximum duration before timing out read of the request"
	WriteTimeout time.Duration //maximum duration before timing out write of the response"
	httpServerL  net.Listener

	Logger       func(string, ...interface{})
	handler      http.Handler
	hasListeners bool
	shutdown     chan struct{}
	doneCh       chan struct{}
	shuttingDown int32
}

// Logf logs message either via defined user logger or via system one if no user logger is defined.
func (s *Server) Logf(f string, args ...interface{}) {
	s.Logger(f, args...)
}

// Fatalf logs message either via defined user logger or via system one if no user logger is defined.
// Exits with non-zero status after printing
func (s *Server) Fatalf(f string, args ...interface{}) {
	log.Fatalf(f, args...)
}

// SetHandler allows for setting a http handler on this server
func (s *Server) SetHandler(handler http.Handler) {

	s.Logger = log.Printf
	s.handler = handler
}

func (s *Server) hasScheme(scheme string) bool {
	schemes := s.EnabledListeners
	if len(schemes) == 0 {
		schemes = defaultSchemes
	}

	for _, v := range schemes {
		if v == scheme {
			return true
		}
	}
	return false
}

// Serve the api
func (s *Server) Serve() (err error) {
	if !s.hasListeners {
		if err = s.Listen(); err != nil {
			return err
		}
	}

	var wg sync.WaitGroup

	if s.hasScheme(schemeHTTP) {
		httpServer := new(http.Server)
		httpServer.MaxHeaderBytes = int(s.MaxHeaderSize)
		httpServer.ReadTimeout = s.ReadTimeout
		httpServer.WriteTimeout = s.WriteTimeout
		httpServer.SetKeepAlivesEnabled(int64(s.KeepAlive) > 0)
		httpServer.Handler = s.handler

		configureServer(httpServer, "http", s.httpServerL.Addr().String())

		wg.Add(2)
		s.Logf("Serving at http://%s", s.httpServerL.Addr())
		go func(l net.Listener) {
			defer wg.Done()
			if err := httpServer.Serve(l); err != nil {
				s.Fatalf("%v", err)
			}
			s.Logf("Stopped serving at http://%s", l.Addr())
		}(s.httpServerL)
		go s.handleShutdown(&wg, httpServer)
	}

	wg.Wait()
	return nil
}

// Listen creates the listeners for the server
func (s *Server) Listen() error {
	if s.hasListeners { // already done this
		return nil
	}

	if s.hasScheme(schemeHTTP) {
		listener, err := net.Listen("tcp", net.JoinHostPort(s.Host, strconv.Itoa(s.Port)))
		if err != nil {
			return err
		}

		h, p, err := x.SplitHostPort(listener.Addr().String())
		if err != nil {
			return err
		}
		s.Host = h
		s.Port = p
		s.httpServerL = listener
	}

	s.hasListeners = true
	return nil
}

// Shutdown server and clean up resources
func (s *Server) Shutdown() {
	if atomic.LoadInt32(&s.shuttingDown) != 0 {
		s.Logf("already shutting down")
		return
	}
	s.shutdown <- struct{}{}
	<-s.doneCh
}

func (s *Server) handleShutdown(wg *sync.WaitGroup, server *http.Server) {
	defer wg.Done()
	for {
		select {
		case <-s.shutdown:
			log.Println("shutting down server")
			atomic.AddInt32(&s.shuttingDown, 1)
			server.Shutdown(context.Background())
			s.doneCh <- struct{}{}
			return
		}
	}
}

// GetHandler returns a handler useful for testing
func (s *Server) GetHandler() http.Handler {
	return s.handler
}

// HTTPListener returns the http listener
func (s *Server) HTTPListener() (net.Listener, error) {
	if !s.hasListeners {
		if err := s.Listen(); err != nil {
			return nil, err
		}
	}
	return s.httpServerL, nil
}

// As soon as server is initialized but not run yet, this function will be called.
// If you need to modify a config, store server instance to stop it individually later, this is the place.
// This function can be called multiple times, depending on the number of serving schemes.
// scheme value will be set accordingly: "http", "https" or "unix"
func configureServer(s *http.Server, scheme, addr string) {
}
