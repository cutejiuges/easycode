package session

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"

	"easycode/internal/domain"
)

const historicalCutLimit = 200

var errHistoricalCutStale = errors.New("historical session branch selection is stale")

// IsHistoricalCutStale 报告选择与当前源实例或已提交范围不再对应。
func IsHistoricalCutStale(err error) bool { return errors.Is(err, errHistoricalCutStale) }

type historicalTip struct {
	source   *JournalWriter
	sequence uint64
	bytes    int64
	digest   string
}

type historicalSnapshot struct {
	tip        historicalTip
	turnStarts map[uint64]int64
}

// HistoricalCut 绑定已验证的回合、首条初始输入及其前面的原始日志边界。
type HistoricalCut struct {
	tip           historicalTip
	turn          domain.TurnID
	input         domain.InputID
	inputSequence uint64
	number        uint64
	sequence      uint64
	bytes         int64
	multiple      bool
}

// TurnID 返回切点之后的第一个回合身份。
func (cut HistoricalCut) TurnID() domain.TurnID { return cut.turn }

// InputID 返回所选回合首条初始输入的身份。
func (cut HistoricalCut) InputID() domain.InputID { return cut.input }

// InputSequence 返回包含所选首条输入的原生提交序号。
func (cut HistoricalCut) InputSequence() uint64 { return cut.inputSequence }

// Number 返回回合在当前线程段中的一基序号。
func (cut HistoricalCut) Number() uint64 { return cut.number }

// HasMultipleInputs 报告所选回合是否包含多条初始输入。
func (cut HistoricalCut) HasMultipleInputs() bool { return cut.multiple }

type historicalReadRequest struct {
	ctx      context.Context
	response chan historicalReadResponse
}

type historicalReadResponse struct {
	loaded LoadResult
	err    error
}

// ReadHistorySnapshot 从源 writer 持有的句柄读取已同步记录，不修复源日志。
func (writer *JournalWriter) ReadHistorySnapshot(ctx context.Context) (LoadResult, error) {
	if err := contextError(ctx); err != nil {
		return LoadResult{}, err
	}
	request := &historicalReadRequest{ctx: ctx, response: make(chan historicalReadResponse, 1)}
	if err := writer.enqueue(ctx, writerRequest{history: request}); err != nil {
		return LoadResult{}, err
	}
	response := <-request.response
	return response.loaded, response.err
}

func (writer *JournalWriter) readHistorySnapshot(ctx context.Context, nextSequence uint64) (LoadResult, error) {
	if err := contextError(ctx); err != nil {
		return LoadResult{}, err
	}
	if writer.poisoned.Load() {
		return LoadResult{}, errBranchSourceUnsafe
	}
	file, ok := writer.file.(journalPrefixSource)
	if !ok {
		return LoadResult{}, fmt.Errorf("session writer cannot read source journal")
	}
	if err := writer.checkBranchSourceSize(file); err != nil {
		return LoadResult{}, err
	}
	turnStarts := make(map[uint64]int64)
	loaded, err := loadJournalRange(ctx, io.NewSectionReader(file, 0, writer.committedBytes), writer.committedBytes, writer.identity.ThreadID, turnStarts)
	if cancelErr := contextError(ctx); cancelErr != nil {
		return LoadResult{}, cancelErr
	}
	if err != nil {
		return LoadResult{}, writer.MarkBranchSourceUnsafe(err)
	}
	if err := writer.checkBranchSourceSize(file); err != nil {
		return LoadResult{}, err
	}
	if loaded.NeedsRepair() || loaded.Identity != writer.identity || loaded.NextSequence != nextSequence || loaded.committedBytes != writer.committedBytes {
		return LoadResult{}, writer.MarkBranchSourceUnsafe(fmt.Errorf("historical session snapshot does not match committed source"))
	}
	loaded.historical = &historicalSnapshot{
		tip:        historicalTip{source: writer, sequence: nextSequence - 1, bytes: writer.committedBytes, digest: loaded.completeDigest},
		turnStarts: turnStarts,
	}
	return loaded, nil
}

type historicalCutCollector struct {
	snapshot *historicalSnapshot
	active   HistoricalCut
	cuts     []HistoricalCut
	number   uint64
	next     int
	more     bool
}

func (history *historicalCutCollector) begin(record Record, hasCommittedInput bool) {
	offset, ok := history.snapshot.turnStarts[record.Sequence]
	if !ok {
		return
	}
	history.number++
	if hasCommittedInput {
		history.active = HistoricalCut{
			tip: history.snapshot.tip, turn: record.TurnID, number: history.number, sequence: record.Sequence - 1, bytes: offset,
		}
	}
}

func (history *historicalCutCollector) bindInput(ids []domain.InputID, sequence uint64) {
	if history.active.turn != "" {
		history.active.input = ids[0]
		history.active.inputSequence = sequence
		history.active.multiple = len(ids) > 1
	}
}

func (history *historicalCutCollector) finish() {
	cut := history.active
	history.active = HistoricalCut{}
	if cut.input == "" {
		return
	}
	if len(history.cuts) < historicalCutLimit {
		history.cuts = append(history.cuts, cut)
		return
	}
	history.cuts[history.next] = cut
	history.next = (history.next + 1) % historicalCutLimit
	history.more = true
}

// HistoricalCuts 返回最近的合格切点，按回合开始顺序排列。第二个返回值指示是否尚有更早切点。
func (plan ReplayPlan) HistoricalCuts() ([]HistoricalCut, bool) {
	if plan.historical == nil {
		return nil, false
	}
	history := plan.historical
	return slices.Concat(history.cuts[history.next:], history.cuts[:history.next]), history.more
}
