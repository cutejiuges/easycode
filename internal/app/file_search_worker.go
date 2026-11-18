package app

import (
	"context"
	"errors"
	"strings"
	"time"

	"easycode/internal/domain"
)

type fileSearchSession struct {
	identity   domain.FileSearchIdentity
	index      []string
	seen       map[string]struct{}
	indexBytes int
	revision   uint64
	report     domain.FileDiscoveryReport
	finished   bool
	matcher    *fileMatcher
}

func sameFileSearchSession(left, right domain.FileSearchIdentity) bool {
	return left.WorkspaceGeneration == right.WorkspaceGeneration && left.SessionGeneration == right.SessionGeneration
}

func (s *workspaceInputSource) run(ctx context.Context) {
	close(s.ready)
	defer func() {
		s.mu.Lock()
		s.closed = true
		s.hasQuery = false
		preparation := s.preparation
		cancelSearch, cancelMatch := s.cancelSearch, s.cancelMatch
		s.mu.Unlock()
		if cancelMatch != nil {
			cancelMatch()
		}
		if cancelSearch != nil {
			cancelSearch()
		}
		if preparation != nil {
			preparation.cancel()
			<-preparation.done
		}
		close(s.results)
		close(s.done)
	}()
	var active *fileSearchSession
	var sessionContext context.Context
	var cancelSession context.CancelFunc
	defer func() {
		if cancelSession != nil {
			cancelSession()
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.wake:
		}
		if ctx.Err() != nil {
			return
		}
		query, ok := s.currentQuery()
		if !ok {
			if cancelSession != nil {
				cancelSession()
			}
			active = nil
			s.clearActiveSearch()
			continue
		}
		if active == nil || !sameFileSearchSession(active.identity, query.Identity()) {
			if cancelSession != nil {
				cancelSession()
			}
			sessionContext, cancelSession = context.WithCancel(ctx)
			if !s.installSearch(query.Identity(), cancelSession) {
				cancelSession()
				continue
			}
			active = &fileSearchSession{identity: query.Identity(), index: make([]string, 0, 256), seen: make(map[string]struct{})}
			discoveryContext, finishDiscovery := context.WithTimeout(sessionContext, 2*time.Second)
			report, err := s.workspace.DiscoverFileNames(discoveryContext, func(paths []string, report domain.FileDiscoveryReport) (bool, error) {
				active.report = report
				proceed := active.appendPaths(paths)
				if !proceed {
					active.report.Issues |= domain.FileDiscoveryIndexLimit
				}
				if err := s.matchLatest(sessionContext, active); err != nil {
					return false, err
				}
				return proceed, nil
			})
			finishDiscovery()
			if sessionContext.Err() != nil {
				active = nil
				continue
			}
			active.report = report
			active.finished = true
			if errors.Is(err, context.DeadlineExceeded) {
				active.report.Issues |= domain.FileDiscoveryDeadline
			} else if err != nil {
				if len(active.index) > 0 {
					active.report.Issues |= domain.FileDiscoveryIOFailure
				} else {
					active.report.Failure = domain.FileUnavailable
					var failure *domain.FileFailure
					if errors.As(err, &failure) && failure.Kind() == domain.FilePlatformUnsupported {
						active.report.Failure = domain.FilePlatformUnsupported
					}
				}
			}
		}
		_ = s.matchLatest(sessionContext, active)
	}
}

func (s *workspaceInputSource) clearActiveSearch() {
	s.mu.Lock()
	s.activeIdentity = domain.FileSearchIdentity{}
	s.cancelSearch = nil
	s.cancelMatch = nil
	s.mu.Unlock()
}
func (s *workspaceInputSource) installSearch(identity domain.FileSearchIdentity, cancel context.CancelFunc) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || !s.hasQuery || !sameFileSearchSession(s.latest.Identity(), identity) {
		return false
	}
	s.activeIdentity = identity
	s.cancelSearch = cancel
	return true
}

func (active *fileSearchSession) appendPaths(paths []string) bool {
	changed := false
	for _, path := range paths {
		if _, found := active.seen[path]; found {
			continue
		}
		ref, err := domain.NewFileReference(path, domain.NewWholeFileSelection())
		if err != nil || ref.Path() != path {
			active.report.Unsupported++
			continue
		}
		cost := len(path) + 96
		if len(active.index) == domain.MaxFileIndexEntries || cost > domain.MaxFileIndexBytes-active.indexBytes {
			if changed {
				active.revision++
			}
			return false
		}
		frozen := strings.Clone(path)
		active.index = append(active.index, frozen)
		active.seen[frozen] = struct{}{}
		active.indexBytes += cost
		changed = true
	}
	if changed {
		active.revision++
	}
	return true
}

func (s *workspaceInputSource) matchLatest(ctx context.Context, active *fileSearchSession) error {
	query, ok := s.currentQuery()
	if !ok || !sameFileSearchSession(query.Identity(), active.identity) {
		return nil
	}
	matchContext, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	valid := s.hasQuery && !s.closed && s.latest.Identity() == query.Identity()
	if valid {
		s.cancelMatch = cancel
	}
	s.mu.Unlock()
	if !valid {
		cancel()
		return nil
	}
	defer cancel()
	if active.matcher == nil || active.matcher.identity != query.Identity() {
		active.matcher = newFileMatcher(query)
	}
	if err := active.matcher.advance(matchContext, active.index); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return nil
	}
	report := active.report
	state := domain.FileSearchProgress
	if active.matcher.limited {
		report.Issues |= domain.FileSearchWorkLimit
	}
	if report.Failure != "" {
		state = domain.FileSearchFailed
	} else if active.finished || active.matcher.limited {
		state = domain.FileSearchComplete
		if report.Issues != 0 {
			state = domain.FileSearchIncomplete
		}
	}
	snapshot, err := domain.NewFileSearchSnapshot(query.Identity(), active.revision, active.matcher.paths(), state, report)
	if err != nil {
		snapshot, err = domain.NewFileSearchSnapshot(query.Identity(), active.revision, nil, domain.FileSearchFailed, domain.FileDiscoveryReport{Failure: domain.FileUnavailable})
		if err != nil {
			return err
		}
	}
	if current, ok := s.currentQuery(); !ok || current.Identity() != query.Identity() {
		return nil
	}
	// worker 是唯一发布者；只保留最新结果，UI 仍必须独立验证全部身份。
	select {
	case <-s.results:
	default:
	}
	s.results <- snapshot
	return nil
}
