package app

import (
	"context"
	chatRuntime "easycode/internal/runtime"
)

// startTUIControlSession 显式启动与 headless 共用的控制适配器。
func startTUIControlSession(ctx context.Context, runtimeValue *chatRuntime.Runtime) (*chatRuntime.ChatSession, error) {
	session := chatRuntime.NewChatSession(runtimeValue)
	if err := session.Start(ctx); err != nil {
		return nil, err
	}
	return session, nil
}
