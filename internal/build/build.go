// Package build 承载编译期注入的版本信息。
//
// 通过 -ldflags 覆盖：
//
//	go build -ldflags "-X github.com/QuantumNous/new-api-cli/internal/build.Version=v1.0.0"
package build

import (
	"fmt"
	"runtime"
)

var (
	// Version 是语义化版本号。
	Version = "dev"
	// Commit 是构建时的 git 短哈希。
	Commit = "none"
	// Date 是构建时间戳。
	Date = "unknown"
)

// GoVersion 返回编译所用的 Go 版本。
func GoVersion() string { return runtime.Version() }

// Platform 返回目标平台标识。
func Platform() string { return fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH) }
