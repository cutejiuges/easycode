package provider

import (
	"bytes"
	"testing"

	"easycode/internal/codec"
	"easycode/internal/domain"
)

func TestInputFileMetadataMandatoryCurrentShapeAndOrder(t *testing.T) {
	id := domain.InputID("01900000-0000-7000-8000-000000000001")
	second := domain.InputID("01900000-0000-7000-8000-000000000002")
	association, err := domain.NewInputAssociation(domain.TurnID(id), domain.InputInitial, 0, []domain.InputID{id, second})
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := NewInputMetadata(association)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := codec.MarshalStable(metadata)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(encoded, []byte(`"files":[]`)) {
		t.Fatal("mandatory empty array missing")
	}
	for _, bad := range [][]byte{
		bytes.Replace(encoded, []byte(`,"files":[]`), nil, 1),
		bytes.Replace(encoded, []byte(`"files":[]`), []byte(`"files":null`), 1),
		bytes.Replace(encoded, []byte(`"files":[]`), []byte(`"files":[],"files":[]`), 1),
		bytes.Replace(encoded, []byte(`"files":[]`), []byte(`"files":[],"unknown":0`), 1),
	} {
		if _, err := DecodeInputMetadata(bad); err == nil {
			t.Fatal("invalid current shape accepted")
		}
	}
	ref, _ := domain.NewFileReference("a", domain.NewWholeFileSelection())
	attachment, _ := domain.FreezeFileAttachment(ref, "file\n")
	files, _ := domain.NewFileAttachments([]domain.FileAttachment{attachment})
	firstGroup, _ := NewInputFileMetadata(id, files)
	secondGroup, _ := NewInputFileMetadata(second, files)
	metadata.Files = []InputFileMetadata{firstGroup, secondGroup}
	if _, err := metadata.Association(); err != nil {
		t.Fatal(err)
	}
	encoded, _ = codec.MarshalStable(metadata)
	decoded, err := DecodeInputMetadata(encoded)
	if err != nil || decoded.FileAttachments(id).Len() != 1 {
		t.Fatal("decode files", err)
	}
	copy := metadata.Clone()
	copy.Files[0] = secondGroup
	if metadata.Files[0].InputID != id {
		t.Fatal("clone shared groups")
	}
	for _, bad := range [][]InputFileMetadata{{secondGroup, firstGroup}, {firstGroup, firstGroup}, {{InputID: id}}, {{InputID: "invalid", Attachments: files}}} {
		copy := metadata.Clone()
		copy.Files = bad
		if _, err := copy.Association(); err == nil {
			t.Fatal("invalid association accepted")
		}
	}
	if _, err := metadata.TextBlocks(id, "@a @b"); err == nil {
		t.Fatal("partial reference collection accepted")
	}
	blocks, err := metadata.TextBlocks(id, "question @a")
	if err != nil || len(blocks) != 2 {
		t.Fatal("blocks", err)
	}
	for _, bad := range []string{`{"input_id":"01900000-0000-7000-8000-000000000001","attachments":[]}`, `{"input_id":"01900000-0000-7000-8000-000000000001","attachments":null}`, `{"input_id":"01900000-0000-7000-8000-000000000001"}`} {
		if _, err := DecodeInputFileMetadata([]byte(bad)); err == nil {
			t.Fatal("empty metadata group accepted")
		}
	}
}
