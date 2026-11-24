package session

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"time"

	"easycode/internal/domain"
)

var errBranchSourceUnsafe = errors.New("session branch source is unsafe")
var errBranchSourceEmpty = errors.New("session branch source has no committed user input")
var errBranchCleanup = errors.New("session branch staging cleanup failed")

// IsBranchSourceUnsafe 报告源提交边界无法证明，当前源不得继续写入。
func IsBranchSourceUnsafe(err error) bool { return errors.Is(err, errBranchSourceUnsafe) }

// IsBranchSourceEmpty 报告源缺少可派生的已提交用户输入。
func IsBranchSourceEmpty(err error) bool { return errors.Is(err, errBranchSourceEmpty) }

// IsBranchCleanupFailure 报告分支暂存资源的清理未能确认成功。
func IsBranchCleanupFailure(err error) bool { return errors.Is(err, errBranchCleanup) }

// BranchPublication 描述同一次分支尝试的持久发布结局。
type BranchPublication uint8

const (
	BranchNotPublished BranchPublication = iota
	BranchPublished
	BranchPublicationUncertain
)

// BranchStage 由宿主的一次交接 worker 独占，直至交出目标租约或完成清理。
type BranchStage struct {
	root        *secureRoot
	directory   *os.File
	lease       *JournalLease
	relative    string
	name        string
	stagingName string
	synced      bool
	attempted   bool
	publication BranchPublication
	closeErr    error
}

// PrepareBranch 用源 writer 持有的句柄复制已确认前缀，并在私有暂存中建立完整的目标记录。
func (repository *Repository) PrepareBranch(ctx context.Context, source *JournalWriter, target domain.ThreadID, title domain.SessionTitle) (_ *BranchStage, _ LoadResult, resultErr error) {
	if err := contextError(ctx); err != nil {
		return nil, LoadResult{}, err
	}
	if source == nil || !target.Valid() || target == source.identity.ThreadID {
		return nil, LoadResult{}, fmt.Errorf("session branch identity is invalid")
	}
	if title != "" && title.Validate() != nil {
		return nil, LoadResult{}, fmt.Errorf("session branch title is invalid")
	}
	root, err := repository.openedRoot()
	if err != nil {
		return nil, LoadResult{}, err
	}
	relative, err := journalRelativePath(target)
	if err != nil {
		return nil, LoadResult{}, err
	}
	stage, err := openBranchStage(root, relative, target)
	if err != nil {
		return nil, LoadResult{}, err
	}
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, stage.Close())
		}
	}()

	if err := stage.writeBranch(ctx, source, title); err != nil {
		return nil, LoadResult{}, err
	}
	loaded, err := NewLoader().Load(ctx, stage.lease)
	if err != nil {
		return nil, LoadResult{}, err
	}
	if loaded.NeedsRepair() {
		return nil, LoadResult{}, fmt.Errorf("prepared branch journal is incomplete")
	}
	return stage, loaded, nil
}

func openBranchStage(root *secureRoot, relative string, thread domain.ThreadID) (*BranchStage, error) {
	directory, err := root.openDirectory(path.Dir(relative), true)
	if err != nil {
		return nil, err
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return nil, errors.Join(fmt.Errorf("generate branch staging name: %w", err), directory.Close())
	}
	name := ".branch-stage-" + hex.EncodeToString(random[:]) + ".tmp"
	file, err := root.openJournal(directory, name, os.O_CREATE|os.O_EXCL|os.O_RDWR|os.O_APPEND, 0o600)
	if err != nil {
		return nil, errors.Join(err, directory.Close())
	}
	if err := checkBranchJournalEntry(directory, name, file); err != nil {
		return nil, errors.Join(err, file.Close(), directory.Close())
	}
	lease, err := acquireJournalLease(file, thread)
	if err != nil {
		return nil, errors.Join(err, directory.Close())
	}
	return &BranchStage{root: root, directory: directory, lease: lease, relative: relative, name: path.Base(relative), stagingName: name}, nil
}

func (stage *BranchStage) writeBranch(ctx context.Context, source *JournalWriter, title domain.SessionTitle) error {
	file, target, err := stage.lease.beginIO()
	if err != nil {
		return err
	}
	defer stage.lease.endIO()
	cut, err := source.branchPrefix(ctx, file)
	if err != nil {
		return err
	}
	if cut.lastSequence == 0 {
		return errBranchSourceEmpty
	}
	loaded, err := loadJournal(ctx, file, cut.identity.ThreadID)
	if err != nil {
		return checkCopyValidationFailure(ctx, source, file, cut, err)
	}
	if loaded.NeedsRepair() {
		return checkCopyValidationFailure(ctx, source, file, cut, fmt.Errorf("copied source journal has an incomplete tail"))
	}
	if loaded.committedBytes != cut.bytes || loaded.completeDigest != cut.digest {
		return fmt.Errorf("branch staging bytes do not match the source copy")
	}
	if loaded.Identity != cut.identity || loaded.NextSequence != cut.lastSequence+1 {
		return source.MarkBranchSourceUnsafe(fmt.Errorf("session branch source cut is inconsistent"))
	}
	metadata, err := NewReplayPlanner().PlanMetadata(loaded)
	if err != nil {
		return source.MarkBranchSourceUnsafe(err)
	}
	if !metadata.hasCommittedInput {
		return errBranchSourceEmpty
	}
	if title == "" {
		title = defaultBranchTitle(metadata.Title)
	}
	payload := ThreadForkedPayload{SourceSequence: cut.lastSequence, SourceSHA256: cut.digest, Title: title}
	draft, err := NewThreadForkedDraft(cut.identity.ThreadID, payload)
	if err != nil {
		return err
	}
	sequence := loaded.NextSequence
	record, err := BuildRecord(Identity{SessionID: cut.identity.SessionID, ThreadID: target}, draft, sequence, time.Now(), sequence, 0, 1)
	if err != nil {
		return err
	}
	if err := contextError(ctx); err != nil {
		return err
	}
	_, encoded, err := EncodeRecord(record)
	if err != nil {
		return err
	}
	line := append(encoded, '\n')
	written, err := file.Write(line)
	if err != nil {
		return fmt.Errorf("write thread fork record: %w", err)
	}
	if written != len(line) {
		return fmt.Errorf("write thread fork record: %w", io.ErrShortWrite)
	}
	return nil
}

func checkCopyValidationFailure(ctx context.Context, source *JournalWriter, file *os.File, cut branchCut, cause error) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	// 只有暂存读取出的完整字节与源复制值一致时，语法错误才归属于源。
	info, err := file.Stat()
	if err != nil || info.Size() != cut.bytes {
		return fmt.Errorf("verify branch staging prefix: %w", errors.Join(cause, err))
	}
	digest := sha256.New()
	count, err := io.Copy(digest, io.NewSectionReader(file, 0, cut.bytes))
	if err != nil || count != cut.bytes || hex.EncodeToString(digest.Sum(nil)) != cut.digest {
		return fmt.Errorf("verify branch staging prefix: %w", errors.Join(cause, err))
	}
	if err := contextError(ctx); err != nil {
		return err
	}
	return source.MarkBranchSourceUnsafe(cause)
}

func defaultBranchTitle(source domain.SessionTitle) domain.SessionTitle {
	if source != "" {
		if title, err := domain.ParseSessionTitle(string(source) + " (Branch)"); err == nil {
			return title
		}
	}
	return domain.SessionTitle("Conversation (Branch)")
}

// Sync 在接纳发布前同步完整候选文件，接纳仍由调用者持有。
func (stage *BranchStage) Sync(ctx context.Context) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if stage == nil || stage.lease == nil || stage.attempted {
		return fmt.Errorf("branch stage is unavailable for sync")
	}
	file, _, err := stage.lease.beginIO()
	if err != nil {
		return err
	}
	defer stage.lease.endIO()
	if err := stage.root.syncBranchFile(file); err != nil {
		return fmt.Errorf("sync branch staging journal: %w", err)
	}
	stage.synced = true
	return contextError(ctx)
}

// Publish 执行已接纳的唯一发布，返回实际持久结果，不因后来取消而中断。
func (stage *BranchStage) Publish() (BranchPublication, error) {
	if stage == nil || stage.lease == nil || !stage.synced {
		return BranchNotPublished, fmt.Errorf("branch stage is unavailable for publication")
	}
	if stage.attempted {
		return stage.publication, fmt.Errorf("branch publication was already attempted")
	}
	stage.attempted = true
	file, _, err := stage.lease.beginIO()
	if err != nil {
		return BranchNotPublished, err
	}
	defer stage.lease.endIO()
	callSecurePathHook(stage.root.hooksForBranch().beforeBranchPublish, stage.relative)
	if err := stage.root.checkBranchRoute(stage.relative, stage.directory, stage.stagingName, file); err != nil {
		return BranchNotPublished, err
	}
	if err := renameBranchNoReplace(stage.directory, stage.stagingName, stage.name); err != nil {
		if !errors.Is(err, fs.ErrExist) && checkBranchJournalEntry(stage.directory, stage.stagingName, file) != nil {
			stage.publication = BranchPublicationUncertain
		}
		return stage.publication, fmt.Errorf("publish branch journal: %w", err)
	}
	stage.publication = BranchPublicationUncertain
	callSecurePathHook(stage.root.hooksForBranch().afterBranchRename, stage.relative)
	if err := stage.root.checkBranchRoute(stage.relative, stage.directory, stage.name, file); err != nil {
		return stage.publication, err
	}
	if err := stage.root.syncBranchDirectory(stage.directory); err != nil {
		return stage.publication, fmt.Errorf("sync branch journal directory: %w", err)
	}
	if err := stage.root.checkBranchRoute(stage.relative, stage.directory, stage.name, file); err != nil {
		return stage.publication, err
	}
	stage.publication = BranchPublished
	return stage.publication, nil
}

// TakeLease 将已确认发布的原目标句柄交给宿主继续装配。
func (stage *BranchStage) TakeLease() (*JournalLease, error) {
	if stage == nil || stage.lease == nil || stage.publication != BranchPublished {
		return nil, fmt.Errorf("branch publication is not confirmed")
	}
	lease := stage.lease
	stage.lease = nil
	return lease, nil
}

// Close 只清理本尝试拥有且仍在暂存名下的文件，不删除正式条目。
func (stage *BranchStage) Close() error {
	if stage == nil {
		return nil
	}
	if stage.directory == nil {
		return stage.closeErr
	}
	var cleanupErr error
	if stage.lease != nil {
		cleanupErr = stage.discardStage()
		cleanupErr = errors.Join(cleanupErr, stage.lease.Close())
		stage.lease = nil
	}
	stage.closeErr = errors.Join(cleanupErr, stage.directory.Close())
	if stage.closeErr != nil {
		stage.closeErr = errors.Join(errBranchCleanup, stage.closeErr)
	}
	stage.directory = nil
	return stage.closeErr
}

func (stage *BranchStage) discardStage() error {
	file, _, err := stage.lease.beginIO()
	if err != nil {
		return err
	}
	defer stage.lease.endIO()
	if err := checkBranchJournalEntry(stage.directory, stage.stagingName, file); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	if err := unlinkBranchStage(stage.directory, stage.stagingName); err != nil {
		return err
	}
	return stage.directory.Sync()
}

// ConfirmBranchDurability 在原目标租约下重新确认可定位分支，早于修复和续写。
func (repository *Repository) ConfirmBranchDurability(ctx context.Context, lease *JournalLease) (resultErr error) {
	if err := contextError(ctx); err != nil {
		return err
	}
	root, err := repository.openedRoot()
	if err != nil {
		return err
	}
	file, thread, err := lease.beginIO()
	if err != nil {
		return err
	}
	defer lease.endIO()
	relative, err := journalRelativePath(thread)
	if err != nil {
		return err
	}
	directory, err := root.openDirectory(path.Dir(relative), false)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, directory.Close()) }()
	name := path.Base(relative)
	if err := root.checkBranchRoute(relative, directory, name, file); err != nil {
		return err
	}
	if err := root.syncBranchFile(file); err != nil {
		return fmt.Errorf("sync restored branch journal: %w", err)
	}
	if err := root.checkBranchRoute(relative, directory, name, file); err != nil {
		return err
	}
	if err := contextError(ctx); err != nil {
		return err
	}
	if err := root.syncBranchDirectory(directory); err != nil {
		return fmt.Errorf("sync restored branch directory: %w", err)
	}
	return errors.Join(root.checkBranchRoute(relative, directory, name, file), contextError(ctx))
}
