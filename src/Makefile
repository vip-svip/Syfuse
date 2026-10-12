# Syfuse 内核构建入口。
#
# 设计原则：每个目标都必须是**可验证的**。`make check` 全绿才允许 `make aar`。
# 不提供 `make install` 之类的隐式动作 —— 部署路径因设备而异，脚本猜不准。

SHELL := /bin/bash
GO ?= $(shell command -v go 2>/dev/null || echo $(HOME)/.workbuddy-ai/toolchain/go/bin/go)

# 隔离的缓存与模块路径，避免污染用户全局 Go 环境
export GOCACHE ?= $(HOME)/.workbuddy-ai/toolchain/go-cache
export GOPATH  ?= $(HOME)/.workbuddy-ai/toolchain/go-path
export GOFLAGS ?= -mod=mod
# 本模块零第三方依赖，因此不需要网络。关掉代理同时起到"证明零依赖"的作用。
export GOPROXY ?= off

OUT := out

.PHONY: help fmt vet test check build aar aar-all clean

help:
	@echo "Syfuse 内核构建目标："
	@echo "  make fmt      格式化源码（gofmt -w）"
	@echo "  make vet      静态检查"
	@echo "  make test     单元测试"
	@echo "  make check    fmt 检查 + vet + test + 零依赖校验（等价于 tests/run_kernel.sh）"
	@echo "  make build    编译本机二进制（快速验证可编译）"
	@echo "  make aar      构建 arm64-v8a AAR（需要 gomobile + NDK）"
	@echo "  make aar-all  构建全部 ABI 的 AAR"
	@echo "  make clean    清理产物"

fmt:
	gofmt -w .

vet:
	$(GO) vet ./...

test:
	$(GO) test ./... -v

# 唯一推荐的验收目标。委托给仓库级脚本，保证本地与 CI 用的是同一套检查。
check:
	@bash ../tests/run_kernel.sh

build:
	$(GO) build ./...

aar:
	@bash tools/build_aar.sh arm64-v8a

aar-all:
	@bash tools/build_aar.sh

clean:
	rm -rf $(OUT)
	$(GO) clean -cache -testcache 2>/dev/null || true
	@echo "已清理 $(OUT) 与 Go 缓存"
