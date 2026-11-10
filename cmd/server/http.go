package main

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/xingshanghe/x-phone/internal/httpapi"
)

type httpServer struct {
	addr    string
	handler *httpapi.Engine
	server  *http.Server
}

func (s *httpServer) listen() error {
	s.server = &http.Server{Addr: s.addr, Handler: s.handler.Router}
	err := s.server.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (s *httpServer) shutdown(ctx context.Context) error {
	if s.server == nil {
		return nil
	}
	shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return s.server.Shutdown(shutdownCtx)
}
