package builtin

import (
	"context"
	"errors"
	"io"
	"os"
	"slices"
	"sort"
	"strings"

	"easycode/internal/domain"
)

type fileDiscovery struct {
	workspace      *Workspace
	report         domain.FileDiscoveryReport
	rules          []fileIgnoreRule
	work           fileWorkBudget
	nameStackBytes int
	stopped        bool
}

// DiscoverFileNames 只读取名称、元数据及本地规则；发布批次不包含文件正文。
// publish 由唯一调用栈串行执行，false 表示宿主索引已到限；本方法返回前关闭所有借用 FD。
func (workspace *Workspace) DiscoverFileNames(ctx context.Context, publish func([]string, domain.FileDiscoveryReport) (bool, error)) (domain.FileDiscoveryReport, error) {
	if ctx == nil || publish == nil || workspace == nil || workspace.handle == nil {
		return domain.FileDiscoveryReport{}, domain.NewFileFailure(domain.FileInvalid)
	}
	if err := ctx.Err(); err != nil {
		return domain.FileDiscoveryReport{}, err
	}
	root, err := workspace.handle.openDirectory("", workspace.hooks)
	if err != nil {
		kind := domain.FileUnavailable
		if errors.Is(err, errWorkspaceUnsupported) {
			kind = domain.FilePlatformUnsupported
		}
		return domain.FileDiscoveryReport{}, domain.NewFileFailure(kind)
	}
	discovery := fileDiscovery{workspace: workspace, work: fileWorkBudget{remaining: 32000000}}
	err = discovery.directory(ctx, root, "", 0, publish)
	discovery.report.IgnoreWork = discovery.work.spent
	if errors.Is(err, errFileDiscoveryWorkLimit) {
		discovery.report.Issues |= domain.FileDiscoveryIgnoreWorkLimit
		err = nil
	}
	return discovery.report, err
}

func (d *fileDiscovery) publish(ctx context.Context, paths []string, publish func([]string, domain.FileDiscoveryReport) (bool, error)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	d.report.IgnoreWork = d.work.spent
	proceed, err := publish(paths, d.report)
	if !proceed && err == nil {
		d.stopped = true
		d.report.Issues |= domain.FileDiscoveryIndexLimit
	}
	if err != nil {
		return err
	}
	return ctx.Err()
}

func (d *fileDiscovery) loadRules(ctx context.Context, directory workspaceDirectory, relative string) (bool, error) {
	for _, name := range []string{".gitignore", ".ignore", ".rgignore"} {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		child, err := directory.openChild(name, d.workspace.hooks)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return false, nil
		}
		if child.kind() != workspaceNodeRegular {
			_ = child.close()
			return false, nil
		}
		remaining := (512 << 10) - d.report.RuleBytes
		content, count, err := d.workspace.readOpenedContent(ctx, child.file(), child.close, min(64<<10, remaining))
		d.report.RuleBytes += count
		if err != nil {
			if ctx.Err() != nil {
				return false, ctx.Err()
			}
			return false, nil
		}
		for content != "" {
			line, rest, _ := strings.Cut(content, "\n")
			content = rest
			rule, present, err := compileIgnoreRule(relative, line)
			if err != nil {
				return false, nil
			}
			if !present {
				continue
			}
			if d.report.RuleCount == 4096 {
				return false, nil
			}
			d.report.RuleCount++
			d.rules = append(d.rules, rule)
		}
	}
	return true, nil
}

func (d *fileDiscovery) names(ctx context.Context, directory workspaceDirectory, publish func([]string, domain.FileDiscoveryReport) (bool, error)) ([]string, int, error) {
	names := make([]string, 0, workspaceNameBatch)
	bytes := 0
	for {
		if err := ctx.Err(); err != nil {
			return nil, 0, err
		}
		remaining := min(4096-len(names), maxWorkspaceEntries-d.report.Visited)
		if remaining == 0 {
			if d.report.Visited == maxWorkspaceEntries {
				d.report.Issues |= domain.FileDiscoveryEntryLimit
				d.stopped = true
			} else {
				// 满额目录仍需有界地确认 EOF，不能把恰好 4096 项误当溢出。
				probe, err := directory.readEntryNames(1)
				d.report.Visited += len(probe)
				if len(probe) == 0 && errors.Is(err, io.EOF) {
					sort.Strings(names)
					return slices.Compact(names), bytes, nil
				}
				if err != nil && !errors.Is(err, io.EOF) {
					if ctx.Err() != nil {
						return nil, 0, ctx.Err()
					}
					d.report.Issues |= domain.FileDiscoveryIOFailure
					return nil, 0, nil
				}
				d.report.Issues |= domain.FileDiscoveryDirectoryLimit
			}
			return nil, 0, nil
		}
		batch, err := directory.readEntryNames(min(workspaceNameBatch, remaining))
		d.report.Visited += len(batch)
		if err != nil && !errors.Is(err, io.EOF) {
			if ctx.Err() != nil {
				return nil, 0, ctx.Err()
			}
			d.report.Issues |= domain.FileDiscoveryIOFailure
			return nil, 0, nil
		}
		for _, name := range batch {
			bytes += len(name) + 16
			if bytes > 1<<20 {
				d.report.Issues |= domain.FileDiscoveryDirectoryLimit
				return nil, 0, nil
			}
			if d.nameStackBytes+bytes > 8<<20 {
				d.report.Issues |= domain.FileDiscoveryStackLimit
				return nil, 0, nil
			}
			names = append(names, name)
		}
		if errors.Is(err, io.EOF) {
			sort.Strings(names)
			return slices.Compact(names), bytes, nil
		}
		if err := d.publish(ctx, nil, publish); err != nil {
			return nil, 0, err
		}
		if d.stopped {
			return nil, 0, nil
		}
	}
}

func (d *fileDiscovery) directory(ctx context.Context, directory workspaceDirectory, relative string, depth int, publish func([]string, domain.FileDiscoveryReport) (bool, error)) (resultErr error) {
	// 取消关闭只触及句柄；原栈等待回调完成后才清理目录状态，超时不伪造完成。
	interrupted := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { defer close(interrupted); _ = directory.interrupt() })
	defer func() {
		if !stop() {
			<-interrupted
		}
		err := directory.close()
		if resultErr == nil && ctx.Err() == nil && err != nil {
			resultErr = domain.NewFileFailure(domain.FileUnavailable)
		}
	}()
	base := len(d.rules)
	defer func() { d.rules = d.rules[:base] }()
	ok, err := d.loadRules(ctx, directory, relative)
	if err != nil {
		return err
	}
	if !ok {
		d.report.Issues |= domain.FileDiscoveryIgnoreUnavailable
		return d.publish(ctx, nil, publish)
	}
	names, nameBytes, err := d.names(ctx, directory, publish)
	if err != nil || d.stopped {
		return err
	}
	d.nameStackBytes += nameBytes
	defer func() { d.nameStackBytes -= nameBytes }()
	batch := make([]string, 0, workspaceNameBatch)
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return err
		}
		if d.stopped {
			return nil
		}
		if isVCSMetadataDirectory(name) {
			continue
		}
		path := name
		if relative != "" {
			path = relative + "/" + name
		}
		ref, err := domain.NewFileReference(path, domain.NewWholeFileSelection())
		if err != nil || ref.Path() != path {
			d.report.Unsupported++
			continue
		}
		child, err := directory.openChild(name, d.workspace.hooks)
		if err != nil {
			d.report.Unsupported++
			continue
		}
		isDirectory := child.kind() == workspaceNodeDirectory
		ignored, matchErr := matchFileIgnore(ctx, d.rules, path, isDirectory, &d.work)
		if matchErr != nil {
			_ = child.close()
			return matchErr
		}
		if ignored {
			_ = child.close()
			continue
		}
		switch child.kind() {
		case workspaceNodeRegular:
			batch = append(batch, path)
			_ = child.close()
		case workspaceNodeDirectory:
			if len(batch) > 0 {
				if err := d.publish(ctx, batch, publish); err != nil {
					_ = child.close()
					return err
				}
				batch = make([]string, 0, workspaceNameBatch)
			}
			if d.stopped {
				_ = child.close()
				return nil
			}
			if depth == maxWorkspaceDepth {
				d.report.Issues |= domain.FileDiscoveryDepthLimit
				_ = child.close()
			} else if err := d.directory(ctx, child.directory(), path, depth+1, publish); err != nil {
				return err
			}
		default:
			d.report.Unsupported++
			_ = child.close()
		}
		if len(batch) == workspaceNameBatch {
			if err := d.publish(ctx, batch, publish); err != nil {
				return err
			}
			batch = make([]string, 0, workspaceNameBatch)
		}
	}
	return d.publish(ctx, batch, publish)
}
