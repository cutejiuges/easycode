package session

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"easycode/internal/domain"
)

// NativeCommitRecord 将通过 lifecycle 校验的 opaque commit 与顺序绑定。
type NativeCommitRecord struct {
	Sequence uint64
	TurnID   domain.TurnID
	Commit   NativeCommitPayload
}

// SampleUsageRecord 将通过 lifecycle 校验的 normalized usage 与顺序绑定。
type SampleUsageRecord struct {
	Sequence uint64
	TurnID   domain.TurnID
	Usage    domain.SampleUsage
}

// ReplayedTurnState 是语义回放后的 current text turn 终态。
type ReplayedTurnState string

const (
	ReplayedTurnCompleted ReplayedTurnState = "completed"
	ReplayedTurnFailed    ReplayedTurnState = "failed"
)

// ReplayedTurn 保存已收口 turn 的最小 lifecycle 投影。
type ReplayedTurn struct {
	TurnID domain.TurnID
	State  ReplayedTurnState
}

// InterruptedTail 表示唯一允许由应用显式补偿的 committed 活动 turn。
type InterruptedTail struct {
	TurnID   domain.TurnID
	Sequence uint64
}

// ReplayPlan 是创建 Provider Conversation 前的不可变语义恢复计划。
type ReplayPlan struct {
	Identity        Identity
	SessionMetadata SessionMetaPayload
	ThreadMetadata  ThreadMetaPayload
	NativeCommits   []NativeCommitRecord
	SampleUsages    []SampleUsageRecord
	Turns           []ReplayedTurn
	OptionalRecords []Record
	InterruptedTail *InterruptedTail
	NextSequence    uint64
	Repair          RepairReport
}

// ReplayPlanner 在 Loader 之后校验强类型 Session lifecycle。
type ReplayPlanner struct{}

// NewReplayPlanner 创建无副作用的语义回放器。
func NewReplayPlanner() *ReplayPlanner {
	return &ReplayPlanner{}
}

// Plan 校验 metadata cardinality、required placement 和当前文本 turn 状态机。
func (*ReplayPlanner) Plan(loaded LoadResult) (ReplayPlan, error) {
	if len(loaded.Records) < 2 {
		return ReplayPlan{}, replayCorrupt("initial metadata batch is missing")
	}
	batches, err := replayBatches(loaded.Records)
	if err != nil {
		return ReplayPlan{}, err
	}
	if err := validateInitialBatch(batches[0]); err != nil {
		return ReplayPlan{}, err
	}
	sessionMetadata, err := DecodeSessionMetaPayload(batches[0][0])
	if err != nil {
		return ReplayPlan{}, replayCorrupt("session metadata payload is invalid: " + err.Error())
	}
	threadMetadata, err := DecodeThreadMetaPayload(batches[0][1])
	if err != nil {
		return ReplayPlan{}, replayCorrupt("thread metadata payload is invalid")
	}
	identity := Identity{SessionID: batches[0][0].SessionID, ThreadID: batches[0][0].ThreadID}
	if err := validateMetadata(identity, sessionMetadata, threadMetadata, batches[0]); err != nil {
		return ReplayPlan{}, err
	}

	plan := ReplayPlan{
		Identity: identity, SessionMetadata: sessionMetadata, ThreadMetadata: threadMetadata,
		NativeCommits: make([]NativeCommitRecord, 0), SampleUsages: make([]SampleUsageRecord, 0),
		Turns:           make([]ReplayedTurn, 0),
		OptionalRecords: make([]Record, 0), NextSequence: loaded.NextSequence, Repair: loaded.Repair,
	}
	var active *InterruptedTail
	for _, batch := range batches[1:] {
		known, optional, filterErr := filterReplayBatch(batch)
		if filterErr != nil {
			return ReplayPlan{}, filterErr
		}
		plan.OptionalRecords = append(plan.OptionalRecords, optional...)
		if len(known) == 0 {
			continue
		}
		for _, record := range known {
			if record.ParentThreadID != "" {
				return ReplayPlan{}, replayCorrupt("root thread record contains a parent thread")
			}
			if record.EventKind == EventSessionMeta || record.EventKind == EventThreadMeta {
				return ReplayPlan{}, replayCorrupt("session metadata is duplicated")
			}
		}

		switch known[0].EventKind {
		case EventTurnStarted:
			if active != nil || len(known) != 1 || known[0].TurnID == "" {
				return ReplayPlan{}, replayCorrupt("turn_started placement is invalid")
			}
			if _, decodeErr := DecodeTurnStartedPayload(known[0]); decodeErr != nil {
				return ReplayPlan{}, replayCorrupt("turn_started payload is invalid")
			}
			active = &InterruptedTail{TurnID: known[0].TurnID, Sequence: known[0].Sequence}
		case EventProviderNativeCommit:
			if active == nil || len(known) != 3 || known[1].EventKind != EventSampleUsage ||
				known[2].EventKind != EventTurnCompleted || known[0].TurnID != active.TurnID ||
				known[1].TurnID != active.TurnID || known[2].TurnID != active.TurnID ||
				known[0].Sequence+1 != known[1].Sequence || known[1].Sequence+1 != known[2].Sequence {
				return ReplayPlan{}, replayCorrupt("completed turn batch is invalid")
			}
			commit, decodeErr := DecodeNativeCommitPayload(known[0])
			if decodeErr != nil {
				return ReplayPlan{}, replayCorrupt("provider native commit payload is invalid")
			}
			usagePayload, decodeErr := DecodeSampleUsagePayload(known[1])
			if decodeErr != nil {
				return ReplayPlan{}, replayCorrupt("sample_usage payload is invalid")
			}
			usage, decodeErr := usagePayload.Domain()
			if decodeErr != nil {
				return ReplayPlan{}, replayCorrupt("sample_usage payload is invalid")
			}
			if _, decodeErr := DecodeTurnCompletedPayload(known[2]); decodeErr != nil {
				return ReplayPlan{}, replayCorrupt("turn_completed payload is invalid")
			}
			plan.NativeCommits = append(plan.NativeCommits, NativeCommitRecord{
				Sequence: known[0].Sequence, TurnID: active.TurnID, Commit: cloneNativeCommit(commit),
			})
			plan.SampleUsages = append(plan.SampleUsages, SampleUsageRecord{
				Sequence: known[1].Sequence, TurnID: active.TurnID, Usage: usage,
			})
			plan.Turns = append(plan.Turns, ReplayedTurn{TurnID: active.TurnID, State: ReplayedTurnCompleted})
			active = nil
		case EventTurnFailed:
			if active == nil || len(known) != 1 || known[0].TurnID != active.TurnID {
				return ReplayPlan{}, replayCorrupt("turn_failed placement is invalid")
			}
			_, decodeErr := DecodeTurnFailedPayload(known[0])
			if decodeErr != nil {
				return ReplayPlan{}, replayCorrupt("turn_failed payload is invalid")
			}
			plan.Turns = append(plan.Turns, ReplayedTurn{TurnID: active.TurnID, State: ReplayedTurnFailed})
			active = nil
		default:
			return ReplayPlan{}, replayCorrupt("session record placement is invalid")
		}
	}
	if active != nil {
		last := loaded.Records[len(loaded.Records)-1]
		if last.Sequence != active.Sequence || last.EventKind != EventTurnStarted {
			return ReplayPlan{}, replayCorrupt("active turn is not the journal tail")
		}
		copy := *active
		plan.InterruptedTail = &copy
	}
	plan.NativeCommits = cloneNativeCommitRecords(plan.NativeCommits)
	plan.OptionalRecords = cloneRecords(plan.OptionalRecords)
	return plan, nil
}

func validateInitialBatch(batch []Record) error {
	if len(batch) != 2 || batch[0].EventKind != EventSessionMeta || batch[1].EventKind != EventThreadMeta ||
		batch[0].BatchID != batch[1].BatchID || batch[0].Sequence != 1 ||
		batch[0].TurnID != "" || batch[1].TurnID != "" ||
		batch[0].ParentThreadID != "" || batch[1].ParentThreadID != "" {
		return replayCorrupt("initial metadata batch is invalid")
	}
	for _, record := range batch {
		if _, exact := LookupDescriptor(record.EventKind, record.PayloadVersion); !exact ||
			record.ReplayRequirement != ReplayRequired {
			return replayCorrupt("initial metadata revision is unsupported")
		}
	}
	return nil
}

func validateMetadata(
	identity Identity,
	sessionMetadata SessionMetaPayload,
	threadMetadata ThreadMetaPayload,
	batch []Record,
) error {
	if identity.SessionID == "" || identity.ThreadID == "" ||
		batch[1].SessionID != identity.SessionID || batch[1].ThreadID != identity.ThreadID ||
		sessionMetadata.RootThreadID != identity.ThreadID || !sessionMetadata.Provider.Valid() ||
		strings.TrimSpace(sessionMetadata.ProviderWire) == "" || strings.TrimSpace(sessionMetadata.Model) == "" ||
		sessionMetadata.SchemaRevision <= 0 || sessionMetadata.CreatedAt.IsZero() ||
		sessionMetadata.CreatedAt.Location() != time.UTC {
		return replayCorrupt("session metadata is invalid")
	}
	if !filepath.IsAbs(sessionMetadata.CreationCWD) || filepath.Clean(sessionMetadata.CreationCWD) != sessionMetadata.CreationCWD {
		return replayCorrupt("session creation cwd is invalid")
	}
	if !threadMetadata.Root || threadMetadata.ParentThreadID != "" {
		return replayCorrupt("root thread metadata is invalid")
	}
	return nil
}

func filterReplayBatch(batch []Record) ([]Record, []Record, error) {
	known := make([]Record, 0, len(batch))
	optional := make([]Record, 0)
	for _, record := range batch {
		if _, exact := LookupDescriptor(record.EventKind, record.PayloadVersion); exact {
			known = append(known, record)
			continue
		}
		if record.ReplayRequirement == ReplayRequired {
			return nil, nil, replayCorrupt("required session payload revision is unsupported")
		}
		optional = append(optional, cloneRecord(record))
	}
	return known, optional, nil
}

func replayBatches(records []Record) ([][]Record, error) {
	batches := make([][]Record, 0)
	for index := 0; index < len(records); {
		size := int(records[index].BatchSize)
		if size <= 0 || index+size > len(records) {
			return nil, replayCorrupt("session batch is incomplete")
		}
		batch := records[index : index+size]
		for offset, record := range batch {
			if record.BatchID != batch[0].BatchID || record.BatchSize != batch[0].BatchSize ||
				record.BatchIndex != uint32(offset) {
				return nil, replayCorrupt("session batch boundary is invalid")
			}
		}
		batches = append(batches, batch)
		index += size
	}
	return batches, nil
}

func cloneNativeCommit(commit NativeCommitPayload) NativeCommitPayload {
	commit.Payload = append([]byte(nil), commit.Payload...)
	return commit
}

func cloneNativeCommitRecords(records []NativeCommitRecord) []NativeCommitRecord {
	cloned := make([]NativeCommitRecord, len(records))
	for index, record := range records {
		cloned[index] = record
		cloned[index].Commit = cloneNativeCommit(record.Commit)
	}
	return cloned
}

func replayCorrupt(message string) error {
	return fmt.Errorf("session replay is corrupted: %s", message)
}
