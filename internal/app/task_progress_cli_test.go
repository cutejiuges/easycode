//go:build darwin || linux

package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"easycode/internal/codec"
	"easycode/internal/domain"
)

func taskScenarioCall(family domain.ProviderFamily, clear bool) writeScenarioCall {
	if family == domain.ProviderAnthropic {
		if clear {
			return writeScenarioCall{"TodoWrite", `{"todos":[]}`}
		}
		return writeScenarioCall{"TodoWrite", `{"todos":[{"content":"检查中文代码","activeForm":"正在检查中文代码","status":"in_progress"},{"content":"验证结果","activeForm":"正在验证结果","status":"pending"}]}`}
	}
	if clear {
		return writeScenarioCall{"update_plan", `{"plan":[],"explanation":"清空后仍保留说明"}`}
	}
	return writeScenarioCall{"update_plan", `{"plan":[{"step":"检查中文代码","status":"in_progress"},{"step":"验证结果","status":"pending"}]}`}
}

// validateTaskServiceSchema 在返回 SSE 之前验证真实请求，拒绝服务器无法表达的 strict 组合。
func validateTaskServiceSchema(family domain.ProviderFamily, body []byte) error {
	type schema struct {
		Type                 string                     `json:"type"`
		Required             []string                   `json:"required"`
		Properties           map[string]json.RawMessage `json:"properties"`
		AdditionalProperties bool                       `json:"additionalProperties"`
	}
	var request struct {
		Tools []struct {
			Name        string `json:"name"`
			Type        string `json:"type"`
			Strict      *bool  `json:"strict"`
			Parameters  schema `json:"parameters"`
			InputSchema schema `json:"input_schema"`
		} `json:"tools"`
	}
	if err := codec.Unmarshal(body, &request); err != nil {
		return err
	}
	name := "update_plan"
	if family == domain.ProviderAnthropic {
		name = "TodoWrite"
	}
	found := false
	for _, facade := range request.Tools {
		if facade.Name != name {
			continue
		}
		if found {
			return fmt.Errorf("duplicate task facade")
		}
		found = true
		spec := facade.Parameters
		field := "plan"
		if family == domain.ProviderOpenAI {
			if facade.Type != "function" || facade.Strict == nil || *facade.Strict {
				return fmt.Errorf("update_plan requires explicit strict false")
			}
			if slices.Contains(spec.Required, "explanation") {
				return fmt.Errorf("explanation must remain optional")
			}
			var explanation struct {
				Type string `json:"type"`
			}
			if err := codec.Unmarshal(spec.Properties["explanation"], &explanation); err != nil || explanation.Type != "string" {
				return fmt.Errorf("explanation must be a non-null string")
			}
		} else {
			spec = facade.InputSchema
			field = "todos"
			if facade.Strict != nil {
				return fmt.Errorf("Anthropic facade contains strict")
			}
		}
		if spec.Type != "object" || spec.AdditionalProperties || len(spec.Required) != 1 || spec.Required[0] != field {
			return fmt.Errorf("task root schema is invalid")
		}
		var array struct {
			Type     string `json:"type"`
			MaxItems int    `json:"maxItems"`
			Items    schema `json:"items"`
		}
		if err := codec.Unmarshal(spec.Properties[field], &array); err != nil || array.Type != "array" || array.MaxItems != 20 || array.Items.Type != "object" || array.Items.AdditionalProperties {
			return fmt.Errorf("task items schema is invalid")
		}
		for _, required := range []string{"status", map[domain.ProviderFamily]string{domain.ProviderOpenAI: "step", domain.ProviderAnthropic: "content"}[family]} {
			if !slices.Contains(array.Items.Required, required) {
				return fmt.Errorf("required task field is missing")
			}
		}
		if family == domain.ProviderAnthropic && !slices.Contains(array.Items.Required, "activeForm") {
			return fmt.Errorf("TodoWrite activeForm is required")
		}
	}
	if !found || len(request.Tools) != 10 {
		return fmt.Errorf("task catalog is incomplete")
	}
	return nil
}

// taskCLIResult 保存真实 CLI 的输出及退出状态。
type taskCLIResult struct {
	stdout, stderr string
	exit           int
}

func runTaskCLI(t *testing.T, binary, home, workspace, input string, arguments ...string) taskCLIResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, binary, arguments...)
	command.Dir = workspace
	for _, value := range os.Environ() {
		key, _, _ := strings.Cut(value, "=")
		if key == "HOME" || strings.HasPrefix(key, "EASYCODE_") {
			continue
		}
		command.Env = append(command.Env, value)
	}
	command.Env = append(command.Env, "HOME="+home)
	command.Stdin = strings.NewReader(input)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	err := command.Run()
	if ctx.Err() != nil {
		t.Fatal("CLI failed to finish and release resources before its deadline")
	}
	if err != nil && command.ProcessState == nil {
		t.Fatalf("start CLI: %v", err)
	}
	return taskCLIResult{stdout.String(), stderr.String(), command.ProcessState.ExitCode()}
}

func buildAppCLI(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(privateAppTempDir(t), "easycode")
	buildContext, cancelBuild := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancelBuild()
	build := exec.CommandContext(buildContext, "go", "build", "-o", binary, "./cmd/easycode")
	build.Dir = root
	account, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	// TestMain 隔离了应用 HOME；构建只复用本机已有 Go 缓存，禁止为测试下载依赖。
	for _, value := range os.Environ() {
		key, _, _ := strings.Cut(value, "=")
		if key != "HOME" && key != "GOPROXY" && key != "GOSUMDB" {
			build.Env = append(build.Env, value)
		}
	}
	build.Env = append(build.Env, "HOME="+account.HomeDir, "GOPROXY=off", "GOSUMDB=off")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, output)
	}
	return binary
}
