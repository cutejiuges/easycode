package tool

import "testing"

func TestDecodeAnthropicEditInputStrictly(t *testing.T) {
	t.Parallel()

	input, err := DecodeAnthropicEditInput([]byte(`{"file_path":"main.go","old_string":"old","new_string":"new","replace_all":true}`))
	if err != nil {
		t.Fatalf("解码Edit: %v", err)
	}
	replacements := input.Replacements()
	if input.FilePath() != "main.go" || len(replacements) != 1 || replacements[0].OldText() != "old" || replacements[0].NewText() != "new" || !replacements[0].ReplaceAll() {
		t.Fatalf("Edit解码结果错误: %#v %#v", input, replacements)
	}

	tests := []string{
		`{}`,
		`{"file_path":"main.go","old_string":"","new_string":"new"}`,
		`{"file_path":"../main.go","old_string":"old","new_string":"new"}`,
		`{"file_path":"main.go","old_string":"old","new_string":"new","extra":true}`,
		`{"file_path":"main.go","file_path":"other.go","old_string":"old","new_string":"new"}`,
		`{"file_path":"main.go","old_string":"old","new_string":"new"}{}`,
	}
	for _, data := range tests {
		if _, decodeErr := DecodeAnthropicEditInput([]byte(data)); decodeErr == nil {
			t.Fatalf("期望拒绝Edit输入: %s", data)
		}
	}
}

func TestParseOpenAIApplyPatchInput(t *testing.T) {
	t.Parallel()

	data := []byte("*** Begin Patch\n*** Update File: main.go\n@@\n package main\n-oldOne\n+newOne\n@@\n-oldTwo\n+newTwo\n*** End Patch\n")
	input, err := ParseOpenAIApplyPatchInput(data)
	if err != nil {
		t.Fatalf("解析apply_patch: %v", err)
	}
	replacements := input.Replacements()
	if input.FilePath() != "main.go" || len(replacements) != 2 {
		t.Fatalf("apply_patch计划错误: %#v", input)
	}
	if !replacements[0].LineBased() || replacements[0].OldText() != "package main\noldOne\n" || replacements[0].NewText() != "package main\nnewOne\n" {
		t.Fatalf("首个hunk错误: %#v", replacements[0])
	}
	if replacements[1].OldText() != "oldTwo\n" || replacements[1].NewText() != "newTwo\n" {
		t.Fatalf("第二个hunk错误: %#v", replacements[1])
	}
}

func TestParseOpenAIApplyPatchRejectsUnsupportedOrIncompleteInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		data string
	}{
		{name: "Add File", data: "*** Begin Patch\n*** Add File: a.go\n+x\n*** End Patch"},
		{name: "Delete File", data: "*** Begin Patch\n*** Delete File: a.go\n*** End Patch"},
		{name: "Move", data: "*** Begin Patch\n*** Update File: a.go\n*** Move to: b.go\n@@\n-a\n+b\n*** End Patch"},
		{name: "多个文件", data: "*** Begin Patch\n*** Update File: a.go\n@@\n-a\n+b\n*** Update File: b.go\n@@\n-a\n+b\n*** End Patch"},
		{name: "缺少End", data: "*** Begin Patch\n*** Update File: a.go\n@@\n-a\n+b"},
		{name: "空hunk", data: "*** Begin Patch\n*** Update File: a.go\n@@\n a\n*** End Patch"},
		{name: "纯插入", data: "*** Begin Patch\n*** Update File: a.go\n@@\n+x\n*** End Patch"},
		{name: "模糊header", data: "*** Begin Patch\n*** Update File: a.go\n@@ function\n-a\n+b\n*** End Patch"},
		{name: "尾随内容", data: "*** Begin Patch\n*** Update File: a.go\n@@\n-a\n+b\n*** End Patch\nnot-empty"},
		{name: "CRLF", data: "*** Begin Patch\r\n*** Update File: a.go\r\n@@\r\n-a\r\n+b\r\n*** End Patch"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := ParseOpenAIApplyPatchInput([]byte(test.data)); err == nil {
				t.Fatal("期望strict parser拒绝输入")
			}
		})
	}
}
