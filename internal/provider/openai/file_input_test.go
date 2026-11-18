package openai

import (
	"bytes"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"easycode/internal/codec"
	"easycode/internal/domain"
	"easycode/internal/provider"
	"easycode/internal/secret"
)

func frozenFileInput(t *testing.T, skill bool) domain.UserInput {
	t.Helper()
	selection, _ := domain.NewLineFileSelection(2, 8)
	ref, _ := domain.NewFileReference("src/a.go", selection)
	a, err := domain.FreezeFileAttachment(ref, "first\r\nsecond\nlast")
	if err != nil {
		t.Fatal(err)
	}
	ref, _ = domain.NewFileReference("docs/a b.md", domain.NewWholeFileSelection())
	b, err := domain.FreezeFileAttachment(ref, "\ufeff@not-recursive\r\n<safe>&\nend")
	if err != nil {
		t.Fatal(err)
	}
	files, err := domain.NewFileAttachments([]domain.FileAttachment{a, b})
	if err != nil {
		t.Fatal(err)
	}
	raw := `inspect @src/a.go#L2-8 and @"docs/a b.md"`
	text := raw
	if skill {
		text = "$qa:demo " + text
	}
	u, _ := domain.NewUserInput("01900000-0000-7000-8000-000000000011", text)
	u, err = u.WithFileAttachments(files)
	if err != nil {
		t.Fatal(err)
	}
	if skill {
		source, _ := domain.NewSkillSource("plugin", "qa", "codex")
		attachment, _ := domain.NewSkillAttachment(source, "qa:demo", domain.SkillDigest("source"), raw, "Frozen skill instructions")
		u, err = u.WithSkillAttachment(attachment)
		if err != nil {
			t.Fatal(err)
		}
	}
	return u
}

func TestFileInputFixedNativeBlocksPrepareDiscardFinalizeAndRestore(t *testing.T) {
	for _, skill := range []bool{false, true} {
		t.Run(fmt.Sprint(skill), func(t *testing.T) {
			instance, err := New(Config{BaseURL: "https://example.invalid", APIKey: secret.New("fixture-key"), Model: "fixture-model"})
			if err != nil {
				t.Fatal(err)
			}
			defer instance.Close()
			c := testConversation(t, instance)
			u := frozenFileInput(t, skill)
			a, _ := domain.NewInputAssociation("01900000-0000-7000-8000-000000000010", domain.InputInitial, 0, []domain.InputID{u.ID()})
			prepared, err := c.PrepareUserInputs(a, []domain.UserInput{u})
			if err != nil {
				t.Fatal(err)
			}
			envelope, err := prepared.Envelope()
			if err != nil {
				t.Fatal(err)
			}
			name := "files"
			if skill {
				name = "skill_files"
			}
			golden, err := os.ReadFile("testdata/" + name + "_input.golden.json")
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(bytes.TrimSpace(golden), envelope.Payload()) {
				t.Fatalf("native golden differs: %s", envelope.Payload())
			}
			preview, _, err := prepared.Preview()
			if err != nil || preview.History().Turns[0].Messages[0].Files.Len() != 2 || len(c.ProjectHistory().Turns) != 0 {
				t.Fatal("candidate leaked or omitted files", err)
			}
			if err := prepared.Discard(); err != nil {
				t.Fatal(err)
			}
			prepared, err = c.PrepareUserInputs(a, []domain.UserInput{u})
			if err != nil {
				t.Fatal(err)
			}
			if err := prepared.Finalize(); err != nil {
				t.Fatal(err)
			}
			if prepared.Finalize() == nil {
				t.Fatal("duplicate finalize accepted")
			}
			value, err := testRestoreAfterPreview(t, instance, "01900000-0000-7000-8000-000000000001", testRestoreRecords(t, []provider.NativeCommitEnvelope{envelope}))
			if err != nil {
				t.Fatal(err)
			}
			restored := value.(*Conversation)
			sampling, err := testSamplingInput(t, instance.config.Inference).WithToolCatalog(testOpenAIToolCatalog(t))
			if err != nil {
				t.Fatal(err)
			}
			before, err := c.compileSamplingRequest(c.history.snapshot(), sampling)
			if err != nil {
				t.Fatal(err)
			}
			after, err := restored.compileSamplingRequest(restored.history.snapshot(), sampling)
			if err != nil {
				t.Fatal(err)
			}
			requestGolden, err := os.ReadFile("testdata/" + name + "_request.golden.json")
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(bytes.TrimSpace(requestGolden), before.Bytes()) {
				t.Fatalf("file request golden differs: %s", before.Bytes())
			}
			if !bytes.Equal(before.Bytes(), after.Bytes()) || domain.ContentFingerprint(before.Bytes()) != domain.ContentFingerprint(after.Bytes()) {
				t.Fatal("restore changed actual request")
			}
			for _, fragment := range []string{"# Attached workspace file", "second\\\\nlast", "@not-recursive"} {
				want := 1
				if fragment == "# Attached workspace file" {
					want = 2
				}
				if strings.Count(string(before.Bytes()), fragment) != want {
					t.Fatal("file body duplicated or missing", fragment)
				}
			}
			view := restored.ProjectHistory()
			message := view.Turns[0].Messages[0]
			if message.Text != u.Text() || message.Files.Len() != 2 || message.HasSkill != skill {
				t.Fatal("projection lost independent values")
			}
			message.Files = domain.FileAttachments{}
			view.Turns[0].Messages[0] = message
			transcript, err := restored.ProjectTranscript()
			if err != nil || transcript.Items[0].Files.Len() != 2 {
				t.Fatal("transcript missing frozen files", err)
			}
			entries := restored.history.snapshot()
			entries[0].InputMetadata.Files[0].Attachments = domain.FileAttachments{}
			if validateNativeHistory(entries) == nil || restored.ProjectHistory().Turns[0].Messages[0].Files.Len() != 2 {
				t.Fatal("clone leaked or invalid metadata accepted")
			}
		})
	}
}

func TestFileFixedRestoreOpaqueCachePoliciesAndEffortIdentity(t *testing.T) {
	fixture, err := os.ReadFile("testdata/files_restore.current.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []domain.OpenAICacheKeyMode{domain.OpenAICacheOmit, domain.OpenAICacheThread} {
		policy, _ := domain.NewCachePolicy(domain.AnthropicCacheDisabled, key, domain.OpenAIHTTPSSE)
		settings, _ := domain.NewOpenAIInference("high", "auto", 16384)
		instance, err := New(Config{BaseURL: "https://example.invalid", Model: "fixture-model", APIKey: secret.New("fixture-key"), CachePolicy: policy, Inference: settings})
		if err != nil {
			t.Fatal(err)
		}
		func() {
			defer instance.Close()
			live := testConversation(t, instance)
			var envelopes []provider.NativeCommitEnvelope
			for _, line := range bytes.Split(bytes.TrimSpace(fixture), []byte("\n")) {
				e, err := provider.NewNativeCommitEnvelope(domain.ProviderOpenAI, "responses", line)
				if err != nil {
					t.Fatal(err)
				}
				entry, err := decodeNativeCommit(e)
				if err != nil {
					t.Fatal(err)
				}
				entry.TurnID = "01900000-0000-7000-8000-000000000010"
				live.history.commit(entry)
				envelopes = append(envelopes, e)
			}
			value, err := testRestoreAfterPreview(t, instance, "01900000-0000-7000-8000-000000000001", testRestoreRecords(t, envelopes))
			if err != nil {
				t.Fatal(err)
			}
			restored := value.(*Conversation)
			sampling, err := testSamplingInput(t, settings).WithToolCatalog(testOpenAIToolCatalog(t))
			if err != nil {
				t.Fatal(err)
			}
			before, err := live.compileSamplingRequest(live.history.snapshot(), sampling)
			if err != nil {
				t.Fatal(err)
			}
			beforeEvidence, err := live.requestEvidence(sampling)
			if err != nil {
				t.Fatal(err)
			}
			footprint, _ := live.HistoryFootprint()
			sources := reasoningCacheSources(t, live)
			for _, c := range []*Conversation{live, restored} {
				view, err := c.ProjectTranscript()
				if err != nil {
					t.Fatal(err)
				}
				for _, item := range view.Items {
					if item.Reasoning != nil && item.Reasoning.Availability() == domain.ReasoningReadable {
						if _, err := item.Reasoning.ReadRange(0, 64<<10, false); err != nil {
							t.Fatal(err)
						}
					}
				}
				after, err := c.compileSamplingRequest(c.history.snapshot(), sampling)
				if err != nil {
					t.Fatal(err)
				}
				e, err := c.requestEvidence(sampling)
				if err != nil {
					t.Fatal(err)
				}
				fp, _ := c.HistoryFootprint()
				if !bytes.Equal(before.Bytes(), after.Bytes()) || e.nativeFingerprint != beforeEvidence.nativeFingerprint || !bytes.Equal(e.properties.Bytes(), beforeEvidence.properties.Bytes()) || !reflect.DeepEqual(footprint, fp) || reasoningCacheSources(t, c) != sources {
					t.Fatal("restore/reading changed native order, footprint or cache")
				}
			}
			left, _ := codec.MarshalStable(live.history.snapshot())
			right, _ := codec.MarshalStable(restored.history.snapshot())
			if !bytes.Equal(left, right) {
				t.Fatal("opaque native history changed")
			}
			for _, sentinel := range []string{"opaque-sentinel", "future-sentinel"} {
				if !bytes.Contains(before.Bytes(), []byte(sentinel)) {
					t.Fatal("lost opaque value", sentinel)
				}
			}
		}()
	}
}

func TestFileEffortActualWirePropertiesAndNativeIdentity(t *testing.T) {
	startup := func() domain.InferenceSettings { s, _ := domain.NewOpenAIInference("high", "auto", 16384); return s }()
	instance, err := New(Config{BaseURL: "https://example.invalid", Model: "fixture-model", APIKey: secret.New("fixture-key"), Inference: startup})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	c := testConversation(t, instance)
	frozen := frozenFileInput(t, false)
	a, _ := domain.NewInputAssociation("01900000-0000-7000-8000-000000000010", domain.InputInitial, 0, []domain.InputID{frozen.ID()})
	p, err := c.PrepareUserInputs(a, []domain.UserInput{frozen})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Finalize(); err != nil {
		t.Fatal(err)
	}
	var bodies, properties [][]byte
	var natives []string
	for _, level := range []domain.EffortLevel{"high", "low", "high"} {
		choice, _ := domain.NewExplicitEffortChoice(level)
		settings, err := choice.Apply(startup)
		if err != nil {
			t.Fatal(err)
		}
		input, err := testSamplingInput(t, settings).WithToolCatalog(testOpenAIToolCatalog(t))
		if err != nil {
			t.Fatal(err)
		}
		body, err := c.compileSamplingRequest(c.history.snapshot(), input)
		if err != nil {
			t.Fatal(err)
		}
		e, err := c.requestEvidence(input)
		if err != nil {
			t.Fatal(err)
		}
		bodies = append(bodies, body.Bytes())
		properties = append(properties, e.properties.Bytes())
		natives = append(natives, e.nativeFingerprint)
	}
	if bytes.Equal(bodies[0], bodies[1]) || bytes.Equal(properties[0], properties[1]) || !bytes.Equal(bodies[0], bodies[2]) || !bytes.Equal(properties[0], properties[2]) || natives[0] != natives[1] || natives[0] != natives[2] || instance.config.Inference != startup {
		t.Fatal("effort or process identity crossed frozen native boundary")
	}
	for _, file := range frozen.FileAttachments().Items() {
		if bytes.Contains(bodies[0], []byte(file.Digest())) {
			t.Fatal("private file digest entered request identity")
		}
	}
}
