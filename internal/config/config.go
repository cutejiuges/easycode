// Package config 负责读取、校验和合并应用配置。
package config

import (
	"fmt"
	"net/url"
	"os"
	"strings"

	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/secret"
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

// LoadFromEnv 从环境变量读取最小 provider 配置。
func LoadFromEnv() Config {
	return Config{
		Provider: Provider{
			Family:  domain.ProviderFamily(strings.ToLower(strings.TrimSpace(os.Getenv("EASYCODE_PROVIDER")))),
			BaseURL: strings.TrimSpace(os.Getenv("EASYCODE_BASE_URL")),
			APIKey:  secret.New(os.Getenv("EASYCODE_API_KEY")),
			Model:   strings.TrimSpace(os.Getenv("EASYCODE_MODEL")),
		},
	}
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
		return fault.Wrap(fault.CodeInvalidConfiguration, "base URL is invalid", err)
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
