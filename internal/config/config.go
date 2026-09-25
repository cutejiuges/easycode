// Package config 负责读取、校验和合并应用配置。
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"easycode/internal/codec"
	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/secret"
)

const (
	// DefaultPathDisplay 是 CLI 和文档展示的默认配置路径。
	DefaultPathDisplay = "~/.config/easycode/config.json"
	maxConfigBytes     = 1 << 20
)

// Provider 描述当前选择的模型服务配置。
type Provider struct {
	Family  domain.ProviderFamily
	BaseURL string
	APIKey  secret.Value
	Model   string
}

// Config 是应用运行所需的顶层配置。
type Config struct {
	Provider Provider
}

type fileConfig struct {
	provider string
	baseURL  string
	apiKey   string
	model    string
}

type fileConfigWire struct {
	Provider string `json:"provider"`
	BaseURL  string `json:"base_url"`
	APIKey   string `json:"api_key"`
	Model    string `json:"model"`
}

// Load 从默认或显式 JSON 文件读取配置，再使用非空环境变量逐字段覆盖。
func Load(path string) (Config, error) {
	resolvedPath, explicit, err := resolvePath(path)
	if err != nil {
		return Config{}, err
	}

	fileValues, found, err := loadFile(resolvedPath, explicit)
	if err != nil {
		return Config{}, err
	}
	loaded := Config{}
	if found {
		loaded = fileValues.toConfig()
	}
	loaded = mergeEnvironment(loaded)
	if err := loaded.ValidateProvider(); err != nil {
		return Config{}, err
	}
	return loaded, nil
}

// LoadFromEnv 从环境变量读取最小 provider 配置。
func LoadFromEnv() Config {
	return mergeEnvironment(Config{})
}

// DefaultPath 返回当前用户 home 下的默认配置路径。
func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return "", fault.New(fault.CodeInvalidConfiguration, "user home directory is unavailable")
	}
	return filepath.Join(home, ".config", "easycode", "config.json"), nil
}

func resolvePath(path string) (string, bool, error) {
	if path == "" {
		resolved, err := DefaultPath()
		return resolved, false, err
	}
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return "", true, fault.New(fault.CodeInvalidConfiguration, "configuration file path is required")
	}
	if trimmed == "~" || strings.HasPrefix(trimmed, "~/") || strings.HasPrefix(trimmed, `~\`) {
		home, err := os.UserHomeDir()
		if err != nil || strings.TrimSpace(home) == "" {
			return "", true, fault.New(fault.CodeInvalidConfiguration, "user home directory is unavailable")
		}
		if trimmed == "~" {
			return filepath.Clean(home), true, nil
		}
		trimmed = filepath.Join(home, trimmed[2:])
	} else if strings.HasPrefix(trimmed, "~") {
		return "", true, fault.New(fault.CodeInvalidConfiguration, "configuration file path has an unsupported home expansion")
	}
	return filepath.Clean(trimmed), true, nil
}

func loadFile(path string, explicit bool) (fileConfig, bool, error) {
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) && !explicit {
			return fileConfig{}, false, nil
		}
		return fileConfig{}, false, fault.New(fault.CodeInvalidConfiguration, "configuration file is unavailable")
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fileConfig{}, false, fault.New(fault.CodeInvalidConfiguration, "configuration file must not be a symbolic link")
	}
	if !info.Mode().IsRegular() {
		return fileConfig{}, false, fault.New(fault.CodeInvalidConfiguration, "configuration file must be a regular file")
	}
	if info.Size() > maxConfigBytes {
		return fileConfig{}, false, fault.New(fault.CodeInvalidConfiguration, "configuration file is too large")
	}

	file, err := os.Open(path)
	if err != nil {
		return fileConfig{}, false, fault.New(fault.CodeInvalidConfiguration, "configuration file is unreadable")
	}
	defer func() { _ = file.Close() }()
	content, err := io.ReadAll(io.LimitReader(file, maxConfigBytes+1))
	if err != nil {
		return fileConfig{}, false, fault.New(fault.CodeInvalidConfiguration, "configuration file is unreadable")
	}
	if len(content) > maxConfigBytes {
		return fileConfig{}, false, fault.New(fault.CodeInvalidConfiguration, "configuration file is too large")
	}
	decoded, err := decodeFile(content)
	if err != nil {
		return fileConfig{}, false, err
	}
	if decoded.apiKey != "" && runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return fileConfig{}, false, fault.New(
			fault.CodeInvalidConfiguration,
			"configuration file permissions must be user-private; run chmod 600",
		)
	}
	return decoded, true, nil
}

func decodeFile(content []byte) (fileConfig, error) {
	decoder := json.NewDecoder(bytes.NewReader(content))
	first, err := decoder.Token()
	if err != nil {
		return fileConfig{}, invalidFileError()
	}
	opening, ok := first.(json.Delim)
	if !ok || opening != '{' {
		return fileConfig{}, invalidFileError()
	}

	fieldCount := 0
	seen := make(map[string]struct{}, 4)
	for decoder.More() {
		keyToken, tokenErr := decoder.Token()
		if tokenErr != nil {
			return fileConfig{}, invalidFileError()
		}
		key, ok := keyToken.(string)
		if !ok {
			return fileConfig{}, invalidFileError()
		}
		if _, exists := seen[key]; exists {
			return fileConfig{}, fault.New(fault.CodeInvalidConfiguration, "configuration file contains a duplicate field")
		}
		seen[key] = struct{}{}

		var rawValue json.RawMessage
		if err := decoder.Decode(&rawValue); err != nil {
			return fileConfig{}, fault.New(fault.CodeInvalidConfiguration, "configuration file field must be a string")
		}
		trimmedValue := bytes.TrimSpace(rawValue)
		if len(trimmedValue) < 2 || trimmedValue[0] != '"' {
			return fileConfig{}, fault.New(fault.CodeInvalidConfiguration, "configuration file field must be a string")
		}
		switch key {
		case "provider", "base_url", "api_key", "model":
		default:
			return fileConfig{}, fault.New(fault.CodeInvalidConfiguration, "configuration file contains an unknown field")
		}
		fieldCount++
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim('}') {
		return fileConfig{}, invalidFileError()
	}
	if fieldCount == 0 {
		return fileConfig{}, fault.New(fault.CodeInvalidConfiguration, "configuration file must not be empty")
	}
	if token, err := decoder.Token(); err != io.EOF || token != nil {
		return fileConfig{}, invalidFileError()
	}
	var wire fileConfigWire
	if err := codec.Unmarshal(content, &wire); err != nil {
		return fileConfig{}, invalidFileError()
	}
	return fileConfig{
		provider: wire.Provider,
		baseURL:  wire.BaseURL,
		apiKey:   wire.APIKey,
		model:    wire.Model,
	}, nil
}

func invalidFileError() error {
	return fault.New(fault.CodeInvalidConfiguration, "configuration file is invalid JSON")
}

func (values fileConfig) toConfig() Config {
	return Config{Provider: Provider{
		Family:  domain.ProviderFamily(strings.ToLower(strings.TrimSpace(values.provider))),
		BaseURL: strings.TrimSpace(values.baseURL),
		APIKey:  secret.New(strings.TrimSpace(values.apiKey)),
		Model:   strings.TrimSpace(values.model),
	}}
}

func mergeEnvironment(base Config) Config {
	if value := strings.TrimSpace(os.Getenv("EASYCODE_PROVIDER")); value != "" {
		base.Provider.Family = domain.ProviderFamily(strings.ToLower(value))
	}
	if value := strings.TrimSpace(os.Getenv("EASYCODE_BASE_URL")); value != "" {
		base.Provider.BaseURL = value
	}
	if value := strings.TrimSpace(os.Getenv("EASYCODE_API_KEY")); value != "" {
		base.Provider.APIKey = secret.New(value)
	}
	if value := strings.TrimSpace(os.Getenv("EASYCODE_MODEL")); value != "" {
		base.Provider.Model = value
	}
	return base
}

// ValidateProvider 校验启动真实模型请求前必须具备的字段。
func (config Config) ValidateProvider() error {
	if !config.Provider.Family.Valid() {
		return fault.New(fault.CodeInvalidConfiguration, "provider must be anthropic or openai")
	}
	if config.Provider.BaseURL == "" {
		return fault.New(fault.CodeInvalidConfiguration, "base URL is required")
	}
	parsedURL, err := url.Parse(config.Provider.BaseURL)
	if err != nil || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") || parsedURL.Host == "" || parsedURL.Hostname() == "" {
		return fault.New(fault.CodeInvalidConfiguration, "base URL is invalid")
	}
	if parsedURL.User != nil || parsedURL.RawQuery != "" || parsedURL.Fragment != "" {
		return fault.New(fault.CodeInvalidConfiguration, "base URL must not contain userinfo, query, or fragment")
	}
	if config.Provider.APIKey.Empty() {
		return fault.New(fault.CodeInvalidConfiguration, "API key is required")
	}
	if config.Provider.Model == "" {
		return fault.New(fault.CodeInvalidConfiguration, "model is required")
	}
	return nil
}

// String 返回可安全输出的配置摘要。
func (config Config) String() string {
	return fmt.Sprintf(
		"provider=%s base_url=%s api_key=%s model=%s",
		config.Provider.Family,
		safeBaseURL(config.Provider.BaseURL),
		config.Provider.APIKey,
		config.Provider.Model,
	)
}

func safeBaseURL(rawURL string) string {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return "<invalid>"
	}
	parsedURL.User = nil
	parsedURL.RawQuery = ""
	parsedURL.Fragment = ""
	return parsedURL.String()
}
