package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"easycode/internal/fault"
	"easycode/internal/session"
	"easycode/internal/session/catalog"
)

// SessionListOptions 描述独立本地列表入口，不包含聊天配置或输入流。
type SessionListOptions struct {
	All                bool
	Limit              int
	SessionDataRoot    string
	SessionCatalogPath string
	Output             io.Writer
	ErrorOutput        io.Writer
}

type sessionListCatalog interface {
	Open(context.Context) error
	Reconcile(context.Context, *session.Repository) (catalog.ReconciliationReport, error)
	List(context.Context, catalog.ListQuery) (catalog.ListResult, error)
	Close() error
}

// sessionListDependencies 将显式存储生命周期与应用编排分离。
type sessionListDependencies struct {
	workingDirectory func() (string, error)
	openRepository   func(string) (*session.Repository, error)
	closeRepository  func(*session.Repository) error
	newCatalog       func(catalog.Config) (sessionListCatalog, error)
}

// ListSessions 协调本地索引并输出列表，不创建聊天资源。
func ListSessions(ctx context.Context, options SessionListOptions) Outcome {
	return listSessions(ctx, options, sessionListDependencies{
		workingDirectory: os.Getwd,
		openRepository:   session.OpenOrCreateRepository,
		closeRepository:  func(repository *session.Repository) error { return repository.Close() },
		newCatalog:       func(config catalog.Config) (sessionListCatalog, error) { return catalog.New(config) },
	})
}

func listSessions(ctx context.Context, options SessionListOptions, deps sessionListDependencies) Outcome {
	if options.Output == nil || options.ErrorOutput == nil {
		return usageFailure("session list output writers are required")
	}
	if options.Limit < 1 || options.Limit > catalog.MaximumListLimit {
		return usageFailure("session list limit must be between 1 and 200")
	}
	if ctx == nil || ctx.Err() != nil {
		return sessionListFailure("session listing was cancelled")
	}
	query := catalog.ListQuery{Scope: catalog.ListAllDirectories, Limit: options.Limit}
	if !options.All {
		cwd, err := deps.workingDirectory()
		if err != nil {
			return sessionListFailure("current working directory is unavailable")
		}
		normalized, err := session.NormalizeCreationCWD(cwd)
		if err != nil {
			return sessionListFailure("session list directory is invalid")
		}
		query.Scope = catalog.ListCurrentDirectory
		query.CreationCWD = normalized
	}
	if err := query.Validate(); err != nil {
		return usageFailure("session list query is invalid")
	}
	paths, err := resolveDataPaths(options.SessionDataRoot, options.SessionCatalogPath)
	if err != nil {
		return sessionListFailure("session list data paths are unavailable")
	}
	result, report, err := loadSessionList(ctx, paths, query, deps)
	if err != nil {
		return sessionListFailure("session listing failed")
	}
	if ctx.Err() != nil {
		return sessionListFailure("session listing was cancelled")
	}
	warning := formatSessionListWarning(report)
	if warning != "" {
		if err := writeSessionListText(options.ErrorOutput, warning); err != nil {
			return sessionListOutputFailure()
		}
	}
	if ctx.Err() != nil {
		return sessionListFailure("session listing was cancelled")
	}
	if err := writeSessionListText(options.Output, formatSessionList(query, result)); err != nil {
		return sessionListOutputFailure()
	}
	if ctx.Err() != nil {
		return sessionListFailure("session listing was cancelled")
	}
	return Outcome{Class: ExitSuccess}
}

func loadSessionList(ctx context.Context, paths dataPaths, query catalog.ListQuery, deps sessionListDependencies) (result catalog.ListResult, report catalog.ReconciliationReport, resultErr error) {
	if err := ctx.Err(); err != nil {
		return result, report, err
	}
	repository, err := deps.openRepository(paths.sessionRoot)
	if err != nil {
		return result, report, err
	}
	defer func() { resultErr = errors.Join(resultErr, deps.closeRepository(repository)) }()
	if err := ctx.Err(); err != nil {
		return result, report, err
	}
	instance, err := deps.newCatalog(catalog.Config{DatabasePath: paths.catalogPath, SessionRoot: paths.sessionRoot})
	if err != nil {
		return result, report, err
	}
	defer func() { resultErr = errors.Join(resultErr, instance.Close()) }()
	if err := ctx.Err(); err != nil {
		return result, report, err
	}
	if err := instance.Open(ctx); err != nil {
		return result, report, err
	}
	if err := ctx.Err(); err != nil {
		return result, report, err
	}
	report, err = instance.Reconcile(ctx, repository)
	if err != nil {
		return result, report, err
	}
	if err := ctx.Err(); err != nil {
		return result, report, err
	}
	result, err = instance.List(ctx, query)
	if err != nil {
		return result, report, err
	}
	return result, report, ctx.Err()
}

func sessionListFailure(message string) Outcome {
	return runtimeFailure(fault.New(fault.CodeSessionCatalog, message))
}
func sessionListOutputFailure() Outcome {
	return runtimeFailure(fault.New(fault.CodeOutput, "write session list output failed"))
}

func writeSessionListText(writer io.Writer, text string) error {
	n, err := io.WriteString(writer, text)
	if err == nil && n != len(text) {
		return io.ErrShortWrite
	}
	return err
}

func formatSessionListWarning(report catalog.ReconciliationReport) string {
	if report.Busy == 0 && report.Invalid == 0 {
		return ""
	}
	return fmt.Sprintf("Warning: storage-wide scan found %d busy and %d invalid journals; indexed metadata may be stale and sessions may be missing.\n", report.Busy, report.Invalid)
}
