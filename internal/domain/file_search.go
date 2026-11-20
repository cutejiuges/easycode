package domain

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	MaxFileIndexEntries      = 50000
	MaxFileIndexBytes        = 16 << 20
	MaxFileSearchResults     = 50
	MaxFileSearchResultBytes = 128 << 10
	MaxFileSearchWork        = 16000000
)

// FileSearchResultPrefix 返回按原有次序符合快照字节预算的候选前缀。
func FileSearchResultPrefix(query string, paths []string) []string {
	// 预留快照及 query 元数据和每条路径的编码开销。
	bytes := 256 + len(query)
	for index, path := range paths {
		bytes += len(path) + 16
		if bytes > MaxFileSearchResultBytes {
			return paths[:index]
		}
	}
	return paths
}

// FileDiscoveryIssue 区分覆盖省略与匹配为空，不属于 prompt 或持久化事实。
type FileDiscoveryIssue uint16

const (
	FileDiscoveryEntryLimit FileDiscoveryIssue = 1 << iota
	FileDiscoveryDirectoryLimit
	FileDiscoveryStackLimit
	FileDiscoveryDepthLimit
	FileDiscoveryIgnoreUnavailable
	FileDiscoveryIgnoreWorkLimit
	FileDiscoveryIOFailure
	FileDiscoveryDeadline
	FileDiscoveryIndexLimit
	FileSearchWorkLimit
)

func (v FileDiscoveryIssue) Validate() error {
	if v & ^(FileDiscoveryEntryLimit|FileDiscoveryDirectoryLimit|FileDiscoveryStackLimit|FileDiscoveryDepthLimit|FileDiscoveryIgnoreUnavailable|FileDiscoveryIgnoreWorkLimit|FileDiscoveryIOFailure|FileDiscoveryDeadline|FileDiscoveryIndexLimit|FileSearchWorkLimit) != 0 {
		return NewFileFailure(FileInvalid)
	}
	return nil
}

// FileDiscoveryReport 只记录有界发现统计，不包含规则正文或绝对根。
type FileDiscoveryReport struct {
	Failure                                                FileFailureKind
	Issues                                                 FileDiscoveryIssue
	Visited, Unsupported, RuleBytes, RuleCount, IgnoreWork int
}

// FileSearchIdentity 同时绑定工作区、候选 session、草稿、目标和请求，不能只比较 query。
type FileSearchIdentity struct {
	WorkspaceGeneration, SessionGeneration, ComposerRevision, RequestGeneration uint64
	Target                                                                      FileMentionTarget
}

func (v FileSearchIdentity) Validate() error {
	t := v.Target
	if v.WorkspaceGeneration == 0 || v.SessionGeneration == 0 || v.RequestGeneration == 0 || t.Start < 0 || t.PathStart != t.Start+1 || t.PathEnd < t.PathStart || t.End < t.PathEnd || t.End > MaxUserInputBytes || !utf8.ValidString(t.Query) || len(t.Query) > MaxFilePathBytes {
		return NewFileFailure(FileInvalid)
	}
	return nil
}

// FileSearchQuery 是宿主的只读元数据请求，不使用 RuntimeCommand。
type FileSearchQuery struct{ identity FileSearchIdentity }

func NewFileSearchQuery(identity FileSearchIdentity) (FileSearchQuery, error) {
	if err := identity.Validate(); err != nil {
		return FileSearchQuery{}, err
	}
	identity.Target.Query = strings.Clone(identity.Target.Query)
	return FileSearchQuery{identity: identity}, nil
}
func (q FileSearchQuery) Identity() FileSearchIdentity { return q.identity }
func (q FileSearchQuery) Query() string                { return q.identity.Target.Query }
func (q FileSearchQuery) Validate() error              { return q.identity.Validate() }

// FileSearchState 是候选覆盖状态，与候选是否可接受分别判断。
type FileSearchState string

const (
	FileSearchProgress   FileSearchState = "searching"
	FileSearchComplete   FileSearchState = "complete"
	FileSearchIncomplete FileSearchState = "incomplete"
	FileSearchFailed     FileSearchState = "error"
)

// FileSearchSnapshot 私有持有完整插入路径，结果返回时复制集合。
type FileSearchSnapshot struct {
	identity      FileSearchIdentity
	indexRevision uint64
	paths         []string
	state         FileSearchState
	report        FileDiscoveryReport
}

func NewFileSearchSnapshot(identity FileSearchIdentity, revision uint64, paths []string, state FileSearchState, report FileDiscoveryReport) (FileSearchSnapshot, error) {
	v := FileSearchSnapshot{identity: identity, indexRevision: revision, paths: paths, state: state, report: report}
	if err := v.Validate(); err != nil {
		return FileSearchSnapshot{}, err
	}
	v.identity.Target.Query = strings.Clone(identity.Target.Query)
	v.paths = make([]string, len(paths))
	for i, path := range paths {
		v.paths[i] = strings.Clone(path)
	}
	return v, nil
}
func (v FileSearchSnapshot) Identity() FileSearchIdentity { return v.identity }
func (v FileSearchSnapshot) IndexRevision() uint64        { return v.indexRevision }
func (v FileSearchSnapshot) Paths() []string              { return append([]string(nil), v.paths...) }
func (v FileSearchSnapshot) State() FileSearchState       { return v.state }
func (v FileSearchSnapshot) Report() FileDiscoveryReport  { return v.report }

func (v FileSearchSnapshot) Validate() error {
	if v.identity.Validate() != nil || v.report.Issues.Validate() != nil || len(v.paths) > MaxFileSearchResults || v.report.Visited < 0 || v.report.Visited > 200000 || v.report.Unsupported < 0 || v.report.RuleBytes < 0 || v.report.RuleBytes > 512<<10 || v.report.RuleCount < 0 || v.report.RuleCount > 4096 || v.report.IgnoreWork < 0 || v.report.IgnoreWork > 32000000 {
		return NewFileFailure(FileInvalid)
	}
	switch v.state {
	case FileSearchProgress:
	case FileSearchComplete:
		if v.report.Issues != 0 {
			return NewFileFailure(FileInvalid)
		}
	case FileSearchIncomplete:
		if v.report.Issues == 0 {
			return NewFileFailure(FileInvalid)
		}
	case FileSearchFailed:
		if v.report.Failure != FileUnavailable && v.report.Failure != FilePlatformUnsupported {
			return NewFileFailure(FileInvalid)
		}
	default:
		return NewFileFailure(FileInvalid)
	}
	if v.state != FileSearchFailed && v.report.Failure != "" {
		return NewFileFailure(FileInvalid)
	}
	for i, path := range v.paths {
		ref, err := NewFileReference(path, NewWholeFileSelection())
		if err != nil || ref.Path() != path {
			return NewFileFailure(FileInvalid)
		}
		for _, previous := range v.paths[:i] {
			if previous == path {
				return NewFileFailure(FileInvalid)
			}
		}
	}
	if len(FileSearchResultPrefix(v.identity.Target.Query, v.paths)) != len(v.paths) {
		return NewFileFailure(FileLimit)
	}
	return nil
}

// Description 只呈现有限原因，不拼接文件系统错误。
func (v FileDiscoveryIssue) Description() string {
	if v == 0 {
		return ""
	}
	labels := make([]string, 0, 10)
	for _, item := range []struct {
		flag  FileDiscoveryIssue
		label string
	}{
		{FileDiscoveryEntryLimit, "entry limit"}, {FileDiscoveryDirectoryLimit, "directory limit"}, {FileDiscoveryStackLimit, "name stack limit"}, {FileDiscoveryDepthLimit, "depth limit"}, {FileDiscoveryIgnoreUnavailable, "ignore_unavailable"}, {FileDiscoveryIgnoreWorkLimit, "ignore work limit"}, {FileDiscoveryIOFailure, "unavailable directory"}, {FileDiscoveryDeadline, "discovery deadline"}, {FileDiscoveryIndexLimit, "index limit"}, {FileSearchWorkLimit, "matching work limit"},
	} {
		if v&item.flag != 0 {
			labels = append(labels, item.label)
		}
	}
	return fmt.Sprintf("Incomplete: %s", strings.Join(labels, ", "))
}
