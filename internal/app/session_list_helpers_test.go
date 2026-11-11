package app

import (
	"context"
	"errors"

	"easycode/internal/session"
	"easycode/internal/session/catalog"
)

type listCatalogStub struct {
	steps    *[]string
	fail     string
	cancelAt string
	cancel   context.CancelFunc
	report   catalog.ReconciliationReport
}

func (s *listCatalogStub) step(name string) error {
	*s.steps = append(*s.steps, name)
	if s.cancelAt == name {
		s.cancel()
	}
	if s.fail == name {
		return errors.New("private storage cause")
	}
	return nil
}
func (s *listCatalogStub) Open(context.Context) error { return s.step("open") }
func (s *listCatalogStub) Reconcile(context.Context, *session.Repository) (catalog.ReconciliationReport, error) {
	return s.report, s.step("reconcile")
}
func (s *listCatalogStub) List(context.Context, catalog.ListQuery) (catalog.ListResult, error) {
	return catalog.ListResult{}, s.step("list")
}
func (s *listCatalogStub) Close() error { return s.step("close") }

type listWriterFunc func([]byte) (int, error)

func (f listWriterFunc) Write(p []byte) (int, error) { return f(p) }
