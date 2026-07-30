package apiserver

import "net/http"

func (s *Server) WrapHandler(wrap func(http.Handler) http.Handler) {
	s.srv.Handler = wrap(s.srv.Handler)
}
