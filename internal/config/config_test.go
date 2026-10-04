package config

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"easycode/internal/fault"
)

const validFile = `{
  "provider": "openai",
  "base_url": "https://example.com/openai/v1",
  "api_key": "file-secret",
  "model": "file-model"
}`

func TestLoadExplicitJSONConfiguration(t *testing.T) {
	clearEnvironment(t)
	path := writeConfig(t, t.TempDir(), validFile, 0o600)

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("load configuration: %v", err)
	}
	if loaded.Provider.Family != "openai" || loaded.Provider.BaseURL != "https://example.com/openai/v1" || loaded.Provider.Model != "file-model" {
		t.Fatalf("loaded configuration: %#v", loaded)
	}
	if loaded.Provider.APIKey.Reveal() != "file-secret" {
		t.Fatal("API key was not loaded")
	}
	if strings.Contains(loaded.String(), "file-secret") {
		t.Fatal("configuration summary leaked API key")
	}
}

func TestLoadExplicitContextTokenBudget(t *testing.T) {
	clearEnvironment(t)
	content := strings.TrimSuffix(validFile, "\n}") + `,
  "context_window_tokens": 200000,
  "reserved_output_tokens": 20000,
  "context_safety_margin_tokens": 4096
}`
	loaded, err := Load(writeConfig(t, t.TempDir(), content, 0o600))
	if err != nil {
		t.Fatalf("load context token budget: %v", err)
	}
	if !loaded.ContextBudget.Enabled() || loaded.ContextBudget.Window() != 200000 ||
		loaded.ContextBudget.ReservedOutput() != 20000 || loaded.ContextBudget.SafetyMargin() != 4096 {
		t.Fatalf("context budget = %#v", loaded.ContextBudget)
	}
	if limit, ok := loaded.ContextBudget.EffectiveInputLimit(); !ok || limit != 175904 {
		t.Fatalf("effective input limit = %d ok %t", limit, ok)
	}
}

func TestLoadContextTokenBudgetDefaultsReservesAndIgnoresEnvironment(t *testing.T) {
	clearEnvironment(t)
	t.Setenv("EASYCODE_CONTEXT_WINDOW_TOKENS", "1")
	content := strings.TrimSuffix(validFile, "\n}") + `,
  "context_window_tokens": 8192
}`
	loaded, err := Load(writeConfig(t, t.TempDir(), content, 0o600))
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ContextBudget.Window() != 8192 || loaded.ContextBudget.ReservedOutput() != 0 || loaded.ContextBudget.SafetyMargin() != 0 {
		t.Fatalf("context budget = %#v", loaded.ContextBudget)
	}

	environmentOnly := LoadFromEnv()
	if environmentOnly.ContextBudget.Enabled() {
		t.Fatal("environment-only configuration enabled a context budget")
	}
}

func TestLoadRejectsInvalidContextTokenBudgetsWithoutLeakingContent(t *testing.T) {
	clearEnvironment(t)
	tests := []struct {
		name   string
		fields string
	}{
		{name: "zero window", fields: `"context_window_tokens":0`},
		{name: "negative window", fields: `"context_window_tokens":-1`},
		{name: "fractional window", fields: `"context_window_tokens":1.5`},
		{name: "string window", fields: `"context_window_tokens":"secret-budget"`},
		{name: "null window", fields: `"context_window_tokens":null`},
		{name: "exponent window", fields: `"context_window_tokens":1e4`},
		{name: "overflowing window", fields: `"context_window_tokens":18446744073709551616`},
		{name: "reserve without window", fields: `"reserved_output_tokens":1`},
		{name: "margin without window", fields: `"context_safety_margin_tokens":1`},
		{name: "reserves equal window", fields: `"context_window_tokens":10,"reserved_output_tokens":5,"context_safety_margin_tokens":5`},
		{name: "reserves exceed window", fields: `"context_window_tokens":10,"reserved_output_tokens":9,"context_safety_margin_tokens":2`},
		{name: "reserves overflow", fields: `"context_window_tokens":18446744073709551615,"reserved_output_tokens":18446744073709551615,"context_safety_margin_tokens":1`},
	}
	base := strings.TrimSuffix(validFile, "\n}")
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := writeConfig(t, t.TempDir(), base+","+test.fields+"}", 0o600)
			_, err := Load(path)
			assertInvalidConfiguration(t, err)
			for _, forbidden := range []string{"file-secret", "secret-budget", test.fields} {
				if strings.Contains(err.Error(), forbidden) {
					t.Fatalf("budget error leaked %q: %v", forbidden, err)
				}
			}
		})
	}
}

func TestLoadRejectsInvalidJSONShapesWithoutLeakingContent(t *testing.T) {
	clearEnvironment(t)
	tests := []struct {
		name    string
		content string
	}{
		{name: "malformed", content: `{"provider":"openai","api_key":"top-secret"`},
		{name: "array", content: `["top-secret"]`},
		{name: "empty object", content: `{}`},
		{name: "unknown field", content: `{"provider":"openai","secret_note":"top-secret"}`},
		{name: "wrong type", content: `{"provider":123,"api_key":"top-secret"}`},
		{name: "null field", content: `{"provider":null,"api_key":"top-secret"}`},
		{name: "duplicate field", content: `{"provider":"openai","provider":"anthropic","api_key":"top-secret"}`},
		{name: "trailing value", content: `{"provider":"openai","api_key":"top-secret"} {}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := writeConfig(t, t.TempDir(), test.content, 0o600)
			_, err := Load(path)
			assertInvalidConfiguration(t, err)
			if strings.Contains(err.Error(), "top-secret") || strings.Contains(err.Error(), test.content) {
				t.Fatalf("unsafe configuration error: %v", err)
			}
		})
	}
}

func TestLoadUsesDefaultPathAndDoesNotCreateMissingFile(t *testing.T) {
	clearEnvironment(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("EASYCODE_PROVIDER", "openai")
	t.Setenv("EASYCODE_BASE_URL", "https://example.com/v1")
	t.Setenv("EASYCODE_API_KEY", "env-secret")
	t.Setenv("EASYCODE_MODEL", "env-model")

	loaded, err := Load("")
	if err != nil {
		t.Fatalf("load default configuration: %v", err)
	}
	if loaded.Provider.Model != "env-model" {
		t.Fatalf("environment fallback: %#v", loaded)
	}
	defaultPath := filepath.Join(home, ".config", "easycode", "config.json")
	if _, err := os.Stat(defaultPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("default configuration was created: %v", err)
	}
}

func TestLoadReadsDefaultPath(t *testing.T) {
	clearEnvironment(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, ".config", "easycode", "config.json")
	writeFile(t, path, validFile, 0o600)

	loaded, err := Load("")
	if err != nil {
		t.Fatalf("load default configuration: %v", err)
	}
	if loaded.Provider.Model != "file-model" {
		t.Fatalf("default configuration: %#v", loaded)
	}
}

func TestLoadExpandsExplicitHomePath(t *testing.T) {
	clearEnvironment(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeFile(t, filepath.Join(home, "custom", "config.json"), validFile, 0o600)

	loaded, err := Load("~/custom/config.json")
	if err != nil {
		t.Fatalf("load home path: %v", err)
	}
	if loaded.Provider.BaseURL != "https://example.com/openai/v1" {
		t.Fatalf("home configuration: %#v", loaded)
	}
}

func TestLoadExplicitMissingFileDoesNotFallBack(t *testing.T) {
	clearEnvironment(t)
	t.Setenv("EASYCODE_PROVIDER", "openai")
	t.Setenv("EASYCODE_BASE_URL", "https://example.com/v1")
	t.Setenv("EASYCODE_API_KEY", "env-secret")
	t.Setenv("EASYCODE_MODEL", "env-model")

	_, err := Load(filepath.Join(t.TempDir(), "missing.json"))
	assertInvalidConfiguration(t, err)
}

func TestLoadRejectsUnsafeFileTypes(t *testing.T) {
	clearEnvironment(t)
	directory := t.TempDir()
	_, err := Load(directory)
	assertInvalidConfiguration(t, err)

	if runtime.GOOS == "windows" {
		return
	}
	target := writeConfig(t, directory, validFile, 0o600)
	link := filepath.Join(directory, "link.json")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("create symlink: %v", err)
	}
	_, err = Load(link)
	assertInvalidConfiguration(t, err)
}

func TestLoadTOCTOUUsesOpenedHandleAcrossDeterministicPathSwap(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("secure opener is unavailable on this platform")
	}
	clearEnvironment(t)
	directory := t.TempDir()
	path := writeConfig(t, directory, validFile, 0o600)
	replacement := filepath.Join(directory, "replacement.json")
	writeFile(t, replacement, strings.ReplaceAll(validFile, "file-model", "replacement-model"), 0o600)
	opener := configFileOpenerFunc(func(openPath string) (*os.File, error) {
		file, err := openConfigFileNoFollow(openPath)
		if err != nil {
			return nil, err
		}
		if err := os.Rename(replacement, openPath); err != nil {
			_ = file.Close()
			return nil, err
		}
		return file, nil
	})

	loaded, found, err := loadFileWithOpener(path, true, opener)
	if err != nil {
		t.Fatalf("load opened configuration handle: %v", err)
	}
	if !found || loaded.model != "file-model" {
		t.Fatalf("loaded replacement path instead of opened handle: %#v", loaded)
	}
}

func TestLoadTOCTOUFailsClosedWhenPathBecomesSymlinkBeforeOpen(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("secure opener is unavailable on this platform")
	}
	clearEnvironment(t)
	directory := t.TempDir()
	path := writeConfig(t, directory, validFile, 0o600)
	outside := writeConfig(t, t.TempDir(), strings.ReplaceAll(validFile, "file-model", "outside-model"), 0o600)
	opener := configFileOpenerFunc(func(openPath string) (*os.File, error) {
		if err := os.Remove(openPath); err != nil {
			return nil, err
		}
		if err := os.Symlink(outside, openPath); err != nil {
			return nil, err
		}
		return openConfigFileNoFollow(openPath)
	})

	_, _, err := loadFileWithOpener(path, true, opener)
	assertInvalidConfiguration(t, err)
}

func TestLoadValidatesTypeAndPermissionsFromOpenedHandle(t *testing.T) {
	clearEnvironment(t)
	directory := t.TempDir()
	_, _, err := loadFileWithOpener("ignored", true, configFileOpenerFunc(func(string) (*os.File, error) {
		return os.Open(directory)
	}))
	assertInvalidConfiguration(t, err)

	if runtime.GOOS == "windows" {
		return
	}
	path := writeConfig(t, t.TempDir(), validFile, 0o644)
	_, _, err = loadFileWithOpener(path, true, configFileOpenerFunc(os.Open))
	assertInvalidConfiguration(t, err)
}

func TestLoadChecksSecretFilePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not expose Unix permission bits")
	}
	clearEnvironment(t)
	tests := []struct {
		mode    os.FileMode
		wantErr bool
	}{
		{mode: 0o600},
		{mode: 0o400},
		{mode: 0o640, wantErr: true},
		{mode: 0o604, wantErr: true},
		{mode: 0o666, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.mode.String(), func(t *testing.T) {
			path := writeConfig(t, t.TempDir(), validFile, test.mode)
			_, err := Load(path)
			if test.wantErr {
				assertInvalidConfiguration(t, err)
				if !strings.Contains(err.Error(), "chmod 600") {
					t.Fatalf("permission hint missing: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("load private configuration: %v", err)
			}
		})
	}
}

func TestLoadAllowsWidePermissionsWithoutFileAPIKey(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not expose Unix permission bits")
	}
	clearEnvironment(t)
	t.Setenv("EASYCODE_API_KEY", "env-secret")
	path := writeConfig(t, t.TempDir(), `{"provider":"openai","base_url":"https://example.com/v1","model":"model"}`, 0o644)
	if _, err := Load(path); err != nil {
		t.Fatalf("load non-secret file: %v", err)
	}
}

func TestLoadEnvironmentOverridesNonEmptyFields(t *testing.T) {
	clearEnvironment(t)
	path := writeConfig(t, t.TempDir(), validFile, 0o600)
	t.Setenv("EASYCODE_MODEL", "env-model")
	t.Setenv("EASYCODE_BASE_URL", "")

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("load merged configuration: %v", err)
	}
	if loaded.Provider.Model != "env-model" || loaded.Provider.BaseURL != "https://example.com/openai/v1" || loaded.Provider.APIKey.Reveal() != "file-secret" {
		t.Fatalf("merged configuration: %#v", loaded)
	}
}

func TestLoadRejectsUnsafeBaseURLWithoutLeakingSecrets(t *testing.T) {
	clearEnvironment(t)
	content := `{"provider":"openai","base_url":"https://user:password@example.com/v1?token=url-secret","api_key":"top-secret","model":"model"}`
	path := writeConfig(t, t.TempDir(), content, 0o600)
	_, err := Load(path)
	assertInvalidConfiguration(t, err)
	for _, secretValue := range []string{"top-secret", "password", "url-secret"} {
		if strings.Contains(err.Error(), secretValue) {
			t.Fatalf("configuration error leaked %q: %v", secretValue, err)
		}
	}
}

func TestLoadFromEnvValidatesProviderWithoutExposingAPIKey(t *testing.T) {
	clearEnvironment(t)
	t.Setenv("EASYCODE_PROVIDER", "openai")
	t.Setenv("EASYCODE_BASE_URL", "https://example.com/openai/v1")
	t.Setenv("EASYCODE_API_KEY", "top-secret")
	t.Setenv("EASYCODE_MODEL", "example-model")

	loaded := LoadFromEnv()
	if err := loaded.ValidateProvider(); err != nil {
		t.Fatalf("validate provider: %v", err)
	}
	if strings.Contains(loaded.String(), "top-secret") {
		t.Fatal("configuration summary leaked API key")
	}
}

func clearEnvironment(t *testing.T) {
	t.Helper()
	for _, name := range []string{"EASYCODE_PROVIDER", "EASYCODE_BASE_URL", "EASYCODE_API_KEY", "EASYCODE_MODEL"} {
		t.Setenv(name, "")
	}
}

func writeConfig(t *testing.T, directory string, content string, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(directory, "config.json")
	writeFile(t, path, content, mode)
	return path
}

func writeFile(t *testing.T, path string, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("create configuration directory: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatalf("write configuration: %v", err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatalf("chmod configuration: %v", err)
	}
}

func assertInvalidConfiguration(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, &fault.Error{Code: fault.CodeInvalidConfiguration}) {
		t.Fatalf("configuration error: %v", err)
	}
}

type configFileOpenerFunc func(string) (*os.File, error)

func (open configFileOpenerFunc) Open(path string) (*os.File, error) {
	return open(path)
}
