package app

import (
	"bytes"
	"context"
	"easycode/internal/session/catalog"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"easycode/internal/config"
	"easycode/internal/domain"
	"easycode/internal/provider"
	"easycode/internal/secret"
	"easycode/internal/session"
	"easycode/internal/tool"
	"easycode/internal/tool/builtin"
	"easycode/internal/tui"
)

func TestPickerRecoveryReloadsFactsAndClassifiesMutation(t *testing.T) {
	original, err := os.ReadFile("../session/testdata/current/input_only_openai.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"removed", "corrupt", "mismatch", "repair_failure", "compensation_failure", "clean_failure", "normal"} {
		t.Run(mode, func(t *testing.T) {
			fixture := original
			if mode == "clean_failure" {
				fixture, err = os.ReadFile("../session/testdata/current/root.jsonl")
				if err != nil {
					t.Fatal(err)
				}
			}
			root, path, thread := installPreviewFixture(t, fixture)
			preview, _, err := previewSession(context.Background(), root, thread, defaultPreviewDependencies())
			if err != nil || len(preview.Messages) == 0 {
				t.Fatal("initial preview failed", err)
			}
			repo, err := session.OpenOrCreateRepository(root)
			if err != nil {
				t.Fatal(err)
			}
			lease, err := repo.Open(context.Background(), thread)
			if err != nil {
				t.Fatal(err)
			}
			loaded, err := session.NewLoader().Load(context.Background(), lease)
			if err != nil {
				t.Fatal(err)
			}
			plan, err := session.NewReplayPlanner().Plan(loaded)
			if err != nil {
				t.Fatal(err)
			}
			lease.Close()
			repo.Close()
			model := plan.SessionMetadata.Model
			switch mode {
			case "removed":
				os.Remove(path)
			case "corrupt":
				os.WriteFile(path, []byte("invalid\n"), 0600)
			case "mismatch":
				model = "different-model"
			case "repair_failure":
				os.WriteFile(path, append(append([]byte(nil), original...), []byte(`{"torn":`)...), 0600)
			}
			before, _ := os.ReadFile(path)
			attempt := &recoveryAttempt{}
			configuration := config.Config{Provider: config.Provider{Family: domain.ProviderOpenAI, Model: model, BaseURL: "https://example.invalid", APIKey: secret.New("fixture-key")}}
			hooks := chatResourceHooks{recovery: attempt}
			if mode == "repair_failure" || mode == "clean_failure" || mode == "compensation_failure" {
				hooks.newExecExecutor = func(*builtin.Workspace, builtin.CommandEnvironment, tool.ExecOutputRenderer) (tool.ExecExecutor, error) {
					return nil, errors.New("injected assembly failure")
				}
			}
			resources, err := openChatResourcesWithHooks(context.Background(), configuration, dataPaths{sessionRoot: root}, string(thread), false, t.TempDir(), hooks)
			if mode == "normal" {
				if err != nil {
					t.Fatal(err)
				}
				if resources.identity.ThreadID != thread {
					t.Fatal("thread changed")
				}
				if err = closePickerResources(resources); err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil {
				closePickerResources(resources)
				t.Fatal("changed facts or injected failure accepted")
			}
			verifyRepo, openErr := session.OpenOrCreateRepository(root)
			if openErr != nil {
				t.Fatal(openErr)
			}
			defer verifyRepo.Close()
			if mode != "removed" {
				freeLease, openErr := verifyRepo.Open(context.Background(), thread)
				if openErr != nil {
					t.Fatal("failure leaked lease", openErr)
				}
				defer freeLease.Close()
				if mode == "repair_failure" || mode == "compensation_failure" {
					recovered, loadErr := session.NewLoader().Load(context.Background(), freeLease)
					if loadErr != nil || recovered.NeedsRepair() {
						t.Fatal("mutation left invalid journal", loadErr)
					}
					replay, replayErr := session.NewReplayPlanner().Plan(recovered)
					if replayErr != nil || replay.InterruptedTail != nil {
						t.Fatal("compensation not committed", replayErr)
					}
					after, readErr := os.ReadFile(path)
					if readErr != nil || !bytes.HasPrefix(after, original) || bytes.Equal(after, original) {
						t.Fatal("original prefix lost or compensation absent", readErr)
					}
				}
			}
			if mode == "repair_failure" {
				if !attempt.fatal() {
					t.Fatal("mutation failure retryable")
				}
			} else if mode == "compensation_failure" {
				// input-only 尾部会追加终态，因此同样属于补偿后的失败。
				if !attempt.fatal() {
					t.Fatal("compensation failure retryable")
				}
			} else {
				if attempt.fatal() {
					t.Fatal("validation rejection marked mutating")
				}
				after, _ := os.ReadFile(path)
				if !bytes.Equal(before, after) {
					t.Fatal("validation changed facts")
				}
			}
		})
	}
}
func TestPickerDiscoveryScopeAndRefresh(t *testing.T) {
	original, err := os.ReadFile("../session/testdata/current/root.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	root, _, _ := installPreviewFixture(t, original)
	if err := os.Chmod(filepath.Dir(root), 0700); err != nil {
		t.Fatal(err)
	}
	discovery := &pickerDiscovery{paths: dataPaths{sessionRoot: root, catalogPath: filepath.Join(filepath.Dir(root), "catalog.sqlite")}, cwd: t.TempDir()}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = discovery.list(ctx, tui.PickerRequest{ID: 1, Operation: tui.PickerList, All: true}); err == nil || discovery.initialized {
		t.Fatal("cancelled initial scan initialized discovery")
	}
	result, err := discovery.list(context.Background(), tui.PickerRequest{ID: 2, Operation: tui.PickerList, All: true})
	if err != nil || len(result.Entries) != 1 || !discovery.initialized {
		t.Fatal("initial scan failed", err)
	}
	result, err = discovery.list(context.Background(), tui.PickerRequest{ID: 3, Operation: tui.PickerList})
	if err != nil || len(result.Entries) != 0 {
		t.Fatal("scope is not exact cwd", err)
	}
	result, err = discovery.list(context.Background(), tui.PickerRequest{ID: 4, Operation: tui.PickerRefresh, All: true})
	if err != nil || len(result.Entries) != 1 {
		t.Fatal("refresh failed", err)
	}
}

func TestPickerDiscoveryRetriesCancelledReconcileAndKeepsWarning(t *testing.T) {
	steps := []string{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stub := &listCatalogStub{steps: &steps, cancelAt: "reconcile", cancel: cancel, report: catalog.ReconciliationReport{Busy: 2, Invalid: 1}}
	deps := defaultSessionListDependencies()
	deps.openRepository = func(string) (*session.Repository, error) { return nil, nil }
	deps.closeRepository = func(*session.Repository) error { return nil }
	deps.newCatalog = func(catalog.Config) (sessionListCatalog, error) { return stub, nil }
	discovery := &pickerDiscovery{cwd: "/workspace", dependencies: &deps}
	if _, err := discovery.list(ctx, tui.PickerRequest{ID: 1, Operation: tui.PickerList}); err == nil || discovery.initialized {
		t.Fatal("cancelled reconcile was accepted")
	}
	stub.cancelAt = ""
	steps = nil
	result, err := discovery.list(context.Background(), tui.PickerRequest{ID: 2, Operation: tui.PickerList, All: true})
	if err != nil || !slices.Contains(steps, "reconcile") || result.Warning == "" {
		t.Fatal("reconcile was not retried", err)
	}
	warning := result.Warning
	steps = nil
	result, err = discovery.list(context.Background(), tui.PickerRequest{ID: 3, Operation: tui.PickerList})
	if err != nil || slices.Contains(steps, "reconcile") || result.Warning != warning {
		t.Fatal("scope query rescanned or lost warning")
	}
	stub.fail = "close"
	result, err = discovery.list(context.Background(), tui.PickerRequest{ID: 4, Operation: tui.PickerRefresh})
	if err == nil || !result.Fatal {
		t.Fatal("cleanup failure was retryable")
	}
}

func TestPickerLateRealRecoveryReleasesLease(t *testing.T) {
	data, err := os.ReadFile("../session/testdata/current/input_only_openai.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	root, _, thread := installPreviewFixture(t, data)
	execute := newPickerExecutor(config.Config{Provider: config.Provider{Family: domain.ProviderOpenAI, Model: "fixture-model", BaseURL: "https://example.invalid", APIKey: secret.New("fixture")}}, dataPaths{sessionRoot: root}, t.TempDir(), nil)
	restored := make(chan pickerExecution, 1)
	release := make(chan struct{})
	owner := newPickerOwner(func(ctx context.Context, r tui.PickerRequest) pickerExecution {
		value := execute(ctx, r)
		restored <- value
		<-release
		return value
	})
	owner.Start(context.Background())
	owner.Submit(tui.PickerRequest{ID: 1, Operation: tui.PickerResume, ThreadID: thread})
	value := <-restored
	if value.resources == nil {
		close(release)
		owner.Stop()
		owner.Wait()
		t.Fatal("real restore failed", value.result.Error)
	}
	owner.Stop()
	close(release)
	if err = owner.Wait(); err != nil {
		t.Fatal(err)
	}
	repo, err := session.OpenOrCreateRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	lease, err := repo.Open(context.Background(), thread)
	if err != nil {
		t.Fatal("late result leaked writer", err)
	}
	lease.Close()
}

func TestPickerResumeReadsJournalAppendedAfterPreview(t *testing.T) {
	ctx := context.Background()
	original, err := os.ReadFile("../session/testdata/current/root.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	root, path, thread := installPreviewFixture(t, original)
	beforePreview, _, err := previewSession(ctx, root, thread, defaultPreviewDependencies())
	if err != nil {
		t.Fatal(err)
	}
	configuration := config.Config{Provider: config.Provider{Family: domain.ProviderOpenAI, Model: "fixture-model", BaseURL: "https://example.invalid", APIKey: secret.New("fixture")}}
	// 另一个真实 owner 在预览释放租约后追加合法输入事实。
	service, err := openSessionService(root)
	if err != nil {
		t.Fatal(err)
	}
	defer service.close()
	factory, err := newProviderResource(configuration)
	if err != nil {
		t.Fatal(err)
	}
	defer factory.Close()
	resumed, err := service.resume(ctx, factory, "responses", "fixture-model", thread)
	if err != nil {
		t.Fatal(err)
	}
	defer resumed.writer.Close(ctx)
	turn := domain.TurnID("01900000-0000-7000-8000-000000000077")
	inputID := domain.InputID("01900000-0000-7000-8000-000000000078")
	started, err := session.NewTurnStartedDraft(turn)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = resumed.writer.AppendBatch(ctx, []session.RecordDraft{started}); err != nil {
		t.Fatal(err)
	}
	association, err := domain.NewInputAssociation(turn, domain.InputInitial, 0, []domain.InputID{inputID})
	if err != nil {
		t.Fatal(err)
	}
	input, err := domain.NewUserInput(inputID, "added after preview")
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := resumed.conversation.(provider.UserInputEncoder).EncodeUserInputs(association, []domain.UserInput{input})
	if err != nil {
		t.Fatal(err)
	}
	inputDraft, err := session.NewTurnInputsCommittedDraft(association)
	if err != nil {
		t.Fatal(err)
	}
	nativeDraft, err := session.NewProviderNativeCommitDraft(turn, session.NativeCommitPayload{Provider: envelope.Family(), Wire: envelope.Wire(), Payload: envelope.Payload()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = resumed.writer.AppendBatch(ctx, []session.RecordDraft{inputDraft, nativeDraft}); err != nil {
		t.Fatal(err)
	}
	if err = resumed.writer.Close(ctx); err != nil {
		t.Fatal(err)
	}
	appended, err := os.ReadFile(path)
	if err != nil || !bytes.HasPrefix(appended, original) || bytes.Equal(appended, original) {
		t.Fatal("writer did not append facts", err)
	}
	execute := newPickerExecutor(configuration, dataPaths{sessionRoot: root}, t.TempDir(), nil)
	got := execute(ctx, tui.PickerRequest{ID: 1, Operation: tui.PickerResume, ThreadID: thread})
	if got.resources == nil {
		t.Fatal("fresh resume failed", got.result.Error)
	}
	defer closePickerResources(got.resources)
	view := got.resources.history
	last := view.Turns[len(view.Turns)-1]
	if last.TurnID != turn || len(last.Messages) != 1 || last.Messages[0].Text != input.Text() || last.Messages[0].InputID != inputID {
		t.Fatal("resume ignored new native input")
	}
	for _, entry := range beforePreview.Messages {
		if entry.TurnID == turn {
			t.Fatal("old preview mutated")
		}
	}
}

func TestPickerDiscoveryCancellationAtCleanupDoesNotInitialize(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	steps := []string{}
	stub := &listCatalogStub{steps: &steps}
	deps := defaultSessionListDependencies()
	deps.openRepository = func(string) (*session.Repository, error) { return nil, nil }
	deps.closeRepository = func(*session.Repository) error { cancel(); return nil }
	deps.newCatalog = func(catalog.Config) (sessionListCatalog, error) { return stub, nil }
	discovery := &pickerDiscovery{cwd: "/workspace", dependencies: &deps}
	if _, err := discovery.list(ctx, tui.PickerRequest{ID: 1, Operation: tui.PickerList}); !errors.Is(err, context.Canceled) || discovery.initialized {
		t.Fatal("cancelled cleanup published discovery", err)
	}
}
