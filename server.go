package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
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

func configureServer(s *http.Server, scheme, addr string) {
}

// NewServer creates a new api server but does not configure it
func NewServer(handler http.Handler, host string, port int) *Server {
	s := new(Server)
	s.SetHandler(handler)
	s.Host = host
	s.Port = port
	return s
}

// Options taken from swagger docs
type Server struct {
	EnabledListeners []string      //the listeners to enable
	CleanupTimeout   time.Duration //grace period for which to wait before shutting down the server
	MaxHeaderSize    int           //controls the maximum number of bytes the server will read parsing the request header's keys and values, including the request line

	SocketPath    string //the unix socket to listen on" default:"/var/run/server.sock"
	domainSocketL net.Listener

	Host            string        //the IP to listen on" default:"localhost" env:"HOST"
	Port            int           //the port to listen on for insecure connections, defaults to a random value" env:"PORT"
	ListenLimit     int           //limit the number of outstanding requests"
	KeepAlive       time.Duration //sets the TCP keep-alive timeouts on accepted connections. It prunes dead TCP connections
	ReadTimeout     time.Duration //maximum duration before timing out read of the request"
	WriteTimeout    time.Duration //maximum duration before timing out write of the response"
	ShutDownTimeout time.Duration //maximum duration for server to wait to shutdown
	httpServerL     net.Listener
	httpServer      *http.Server

	Logger       func(string, ...interface{})
	handler      http.Handler
	hasListeners bool
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

	if s.hasScheme(schemeHTTP) {
		httpServer := new(http.Server)
		httpServer.MaxHeaderBytes = int(s.MaxHeaderSize)
		httpServer.ReadTimeout = s.ReadTimeout
		httpServer.WriteTimeout = s.WriteTimeout
		httpServer.SetKeepAlivesEnabled(int64(s.KeepAlive) > 0)
		httpServer.Handler = s.handler

		configureServer(httpServer, "http", s.httpServerL.Addr().String())
		s.Logf("Serving at http://%s", s.httpServerL.Addr())
		s.httpServer = httpServer

		go func(l net.Listener) {
			if err := s.httpServer.Serve(l); err != nil {
				s.Fatalf("%v", err)
			}
			s.Logf("Stopped serving at http://%s", l.Addr())
		}(s.httpServerL)
	}

	// Listen to various error signals for shutdown/restart of server
	signalCh := make(chan os.Signal, 1024)
	signal.Notify(signalCh, syscall.SIGHUP, syscall.SIGUSR2, syscall.SIGINT, syscall.SIGQUIT, syscall.SIGKILL, os.Interrupt)
	for {
		select {
		case sig := <-signalCh:
			fmt.Printf("%v signal received.\n", sig)
			switch sig {
			case syscall.SIGHUP:
				// Fork a child process.
				addr := net.JoinHostPort(s.Host, strconv.Itoa(s.Port))
				p, err := forkChild(addr, s.httpServerL)
				if err != nil {
					fmt.Printf("Unable to fork child: %v.\n", err)
					continue
				}
				fmt.Printf("Forked child %v.\n", p.Pid)

				// Return any errors during shutdown.
				return s.handleShutdown()
			case syscall.SIGUSR2:
				// Fork a child process.
				addr := net.JoinHostPort(s.Host, strconv.Itoa(s.Port))
				p, err := forkChild(addr, s.httpServerL)
				if err != nil {
					fmt.Printf("Unable to fork child: %v.\n", err)
					continue
				}

				// Print the PID of the forked process and keep waiting for more signals.
				fmt.Printf("Forked child %v.\n", p.Pid)
			case syscall.SIGINT, syscall.SIGQUIT, syscall.SIGKILL:
				// Return any errors during shutdown.
				return s.handleShutdown()

			default:
				// Return any errors during shutdown.
				return s.handleShutdown()
			}
		}
	}

}

// Listen creates the listeners for the server
func (s *Server) Listen() error {
	if s.hasListeners { // already done this
		return nil
	}

	if s.hasScheme(schemeHTTP) {
		addr := net.JoinHostPort(s.Host, strconv.Itoa(s.Port))
		listener, err := createOrImportListener(addr)
		if err != nil {
			return err
		}

		_, _, err = SplitHostPort(listener.Addr().String())
		if err != nil {
			return err
		}
		s.httpServerL = listener
	}

	s.hasListeners = true
	return nil
}

// Shutdown server and clean up resources
func (s *Server) handleShutdown() error {
	if s.ShutDownTimeout > 0 {
		ctx, cancel := context.WithTimeout(context.Background(), s.ShutDownTimeout)
		defer cancel()
		return s.httpServer.Shutdown(ctx)
	} else {
		return s.httpServer.Shutdown(context.Background())
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

type listener struct {
	Addr     string `json:"addr"`
	FD       int    `json:"fd"`
	Filename string `json:"filename"`
}

func createListener(addr string) (net.Listener, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}

	return ln, nil
}

func importListener(addr string) (net.Listener, error) {
	// Extract the encoded listener metadata from the environment.
	listenerEnv := os.Getenv("LISTENER")
	if listenerEnv == "" {
		return nil, fmt.Errorf("unable to find LISTENER environment variable")
	}

	// Unmarshal the listener metadata.
	var l listener
	err := json.Unmarshal([]byte(listenerEnv), &l)
	if err != nil {
		return nil, err
	}
	if l.Addr != addr {
		return nil, fmt.Errorf("unable to find listener for %v", addr)
	}

	// The file has already been passed to this process, extract the file
	// descriptor and name from the metadata to rebuild/find the *os.File for
	// the listener.
	listenerFile := os.NewFile(uintptr(l.FD), l.Filename)
	if listenerFile == nil {
		return nil, fmt.Errorf("unable to create listener file: %v", err)
	}
	defer listenerFile.Close()

	// Create a net.Listener from the *os.File.
	ln, err := net.FileListener(listenerFile)
	if err != nil {
		return nil, err
	}
	return ln, nil
}

func createOrImportListener(addr string) (net.Listener, error) {
	// Try and import a listener for addr. If it's found, use it.
	ln, err := importListener(addr)
	if err == nil {
		fmt.Printf("Imported listener file descriptor for %v.\n", addr)
		return ln, nil
	}

	// No listener was imported, that means this process has to create one.
	ln, err = createListener(addr)
	if err != nil {
		return nil, err
	}

	fmt.Printf("Created listener file descriptor for %v.\n", addr)
	return ln, nil
}

func getListenerFile(ln net.Listener) (*os.File, error) {
	switch t := ln.(type) {
	case *net.TCPListener:
		return t.File()
	case *net.UnixListener:
		return t.File()
	}
	return nil, fmt.Errorf("unsupported listener: %T", ln)
}

func forkChild(addr string, ln net.Listener) (*os.Process, error) {
	// Get the file descriptor for the listener and marshal the metadata to pass
	// to the child in the environment.
	lnFile, err := getListenerFile(ln)
	if err != nil {
		return nil, err
	}
	defer lnFile.Close()
	l := listener{
		Addr:     addr,
		FD:       3,
		Filename: lnFile.Name(),
	}
	listenerEnv, err := json.Marshal(l)
	if err != nil {
		return nil, err
	}

	// Pass stdin, stdout, and stderr along with the listener to the child.
	files := []*os.File{
		os.Stdin,
		os.Stdout,
		os.Stderr,
		lnFile,
	}

	// Get current environment and add in the listener to it.
	environment := append(os.Environ(), "LISTENER="+string(listenerEnv))

	// Get current process name and directory.
	execName, err := os.Executable()
	if err != nil {
		return nil, err
	}
	execDir := filepath.Dir(execName)

	// Spawn child process.
	p, err := os.StartProcess(execName, []string{execName}, &os.ProcAttr{
		Dir:   execDir,
		Env:   environment,
		Files: files,
		Sys:   &syscall.SysProcAttr{},
	})
	if err != nil {
		return nil, err
	}

	return p, nil
}

// SplitHostPort splits a network address into a host and a port.
// The port is -1 when there is no port to be found
func SplitHostPort(addr string) (host string, port int, err error) {
	h, p, err := net.SplitHostPort(addr)
	if err != nil {
		return "", -1, err
	}
	if p == "" {
		return "", -1, &net.AddrError{Err: "missing port in address", Addr: addr}
	}

	pi, err := strconv.Atoi(p)
	if err != nil {
		return "", -1, err
	}
	return h, pi, nil
}
