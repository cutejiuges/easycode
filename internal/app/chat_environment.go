package app

import (
	"context"
	"errors"
	"os"

	"easycode/internal/context/projectinstructions"
	"easycode/internal/domain"
	"easycode/internal/extension/plugin"
	"easycode/internal/extension/skill"
	"easycode/internal/fault"
	"easycode/internal/session"
	"easycode/internal/session/artifact"
	"easycode/internal/tool/builtin"
)

// chatEnvironment 由终端宿主持有，直到最后一个借用它的会话完成清理。
type chatEnvironment struct {
	instructions *domain.ProjectInstructionsSnapshot
	roots        *chatRoots
	extensions   *chatExtensions
}

type chatRoots struct {
	repository *session.Repository
	layout     *artifact.Layout
	workspace  *builtin.Workspace
}

type chatExtensions struct {
	skills *skill.Local
	owner  *plugin.Sources
	views  []domain.PluginListEntry
	hooks  domain.HookSnapshot
}

func projectInstructionSnapshot(cwd string, environment *chatEnvironment) (domain.ProjectInstructionsSnapshot, error) {
	if environment != nil && environment.instructions != nil {
		return *environment.instructions, nil
	}
	loader, err := projectinstructions.NewLoader(projectinstructions.DefaultMaxBytes)
	if err != nil {
		return domain.ProjectInstructionsSnapshot{}, fault.New(fault.CodeProjectInstructionsRead, "project instruction loader is invalid")
	}
	snapshot, err := loader.Load(cwd)
	if err == nil && environment != nil {
		environment.instructions = &snapshot
	}
	return snapshot, err
}

func acquireChatRoots(ctx context.Context, dataRoot, cwd string, environment *chatEnvironment, cleanup *resourceCleanup) (*chatRoots, error) {
	if environment != nil && environment.roots != nil {
		if err := environment.roots.layout.Check(ctx); err != nil {
			return nil, fault.New(fault.CodeInvalidConfiguration, "unsafe_storage_layout")
		}
		return environment.roots, nil
	}
	r := &chatRoots{}
	cleanup.add(func() error {
		if environment != nil && environment.roots == r {
			return nil
		}
		return r.close()
	})
	var err error
	if r.repository, err = session.OpenOrCreateRepository(dataRoot); err != nil {
		return nil, err
	}
	if r.layout, err = openCommandArtifactBoundary(ctx, r.repository.RootPath(), cwd); err != nil {
		return nil, err
	}
	if r.workspace, err = builtin.OpenWorkspace(cwd); err != nil {
		return nil, fault.New(fault.CodeInvalidConfiguration, "workspace is unavailable")
	}
	storage, storageErr := r.repository.InspectRoot()
	workspace, workspaceErr := r.workspace.InspectRoot()
	if storageErr != nil || workspaceErr != nil || r.layout.CheckRoots(storage, workspace) != nil {
		return nil, fault.New(fault.CodeInvalidConfiguration, "unsafe_storage_layout")
	}
	if environment != nil {
		environment.roots = r
	}
	return r, nil
}

func (r *chatRoots) close() error {
	if r == nil {
		return nil
	}
	return errors.Join(r.workspace.Close(), r.layout.Close(), r.repository.Close())
}

func loadChatExtensions(ctx context.Context, source *skill.Local, layout *artifact.Layout, cwd string, options chatResourceHooks, cleanup *resourceCleanup) (*chatExtensions, error) {
	pluginOptions := options.pluginOptions
	pluginOptions.Boundary = layout.CheckSource
	pluginSources := plugin.NewSources(pluginOptions)
	cleanup.add(func() error { return pluginSources.Close(context.Background()) })
	pluginCWD := options.pluginCWD
	if pluginCWD == "" {
		pluginCWD = cwd
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	pluginViews, contributions, err := pluginSources.Load(ctx, pluginCWD, options.pluginDirs)
	if err != nil {
		return nil, err
	}
	if err := source.Load(ctx, cwd, home, contributions, pluginViews); err != nil {
		return nil, err
	}
	views, err := pluginList(pluginViews, source.PluginSummaries())
	if err != nil {
		return nil, err
	}
	hooks, err := loadChatHookSnapshot(ctx, cwd, home, options)
	if err != nil {
		return nil, err
	}
	return &chatExtensions{skills: source, owner: pluginSources, views: views, hooks: hooks}, nil
}

func (e *chatEnvironment) close() error {
	var err error
	if e.extensions != nil {
		err = e.extensions.owner.Close(context.Background())
	}
	return errors.Join(err, e.roots.close())
}
