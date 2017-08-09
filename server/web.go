package server

import (
	"fmt"
	"net/http"

	"github.com/Sirupsen/logrus"
	"github.com/rancher/vxlan/vxlan"
)

// Server structure is used to the store backend information
type Server struct {
	V *vxlan.Vxlan
}

// ListenAndServe is used to setup ping and reload handlers and
// start listening on the specified port
func (s *Server) ListenAndServe(listen string) error {
	http.HandleFunc("/ping", s.ping)
	http.HandleFunc("/v1/reload", s.reload)
	http.HandleFunc("/v1/loglevel", s.loglevel)
	logrus.Infof("Listening on %s", listen)
	err := http.ListenAndServe(listen, nil)
	if err != nil {
		logrus.Errorf("got error while ListenAndServe: %v", err)
	}
	return err
}

func (s *Server) ping(rw http.ResponseWriter, req *http.Request) {
	logrus.Debug("Received ping request")
	rw.Write([]byte("OK"))
}

func (s *Server) reload(rw http.ResponseWriter, req *http.Request) {
	logrus.Debug("Received reload request")
	msg := "Reloaded Configuration\n"
	if err := s.V.Reload(); err != nil {
		rw.WriteHeader(http.StatusInternalServerError)
		msg = fmt.Sprintf("Failed to reload configuration: %v\n", err)
	}

	rw.Write([]byte(msg))
}

func (s *Server) loglevel(rw http.ResponseWriter, req *http.Request) {
	// curl -X POST -d "level=debug" localhost:8111/v1/loglevel
	logrus.Debug("Received loglevel request")
	if req.Method == http.MethodGet {
		level := logrus.GetLevel().String()
		rw.Write([]byte(fmt.Sprintf("loglevel: %s\n", level)))
	}

	if req.Method == http.MethodPost {
		if err := req.ParseForm(); err != nil {
			rw.WriteHeader(http.StatusInternalServerError)
			rw.Write([]byte(fmt.Sprintf("Failed to parse form: %v\n", err)))
		}
		level, err := logrus.ParseLevel(req.Form.Get("level"))
		if err != nil {
			rw.WriteHeader(http.StatusInternalServerError)
			rw.Write([]byte(fmt.Sprintf("Failed to parse loglevel: %v\n", err)))
		} else {
			logrus.SetLevel(level)
			rw.Write([]byte("OK"))
		}
	}
}
