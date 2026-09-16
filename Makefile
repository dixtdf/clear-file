# clear-file 文件清理器 - 常用命令

IMAGE ?= file-cleaner
TAG   ?= latest
ADDR  ?= :6888
ROOT  ?= /mnt

# 版本号：默认读根目录 VERSION（改版本号用 .\set-version.ps1 0.1.1）
VERSION ?= $(shell cat VERSION 2>/dev/null || echo dev)
LDFLAGS  = -s -w -X file-cleaner/internal/version.Version=$(VERSION)

.PHONY: help
help:
	@echo "make dev-web     - 启动前端开发服务器 (localhost:5173, /api 代理到 :6888)"
	@echo "make dev-api     - 本地运行后端 (root=$(ROOT) addr=$(ADDR))"
	@echo "make web         - 构建前端到 web/dist"
	@echo "make build       - 构建单文件二进制 ./file-cleaner (内嵌前端)"
	@echo "make image       - 构建 Docker 镜像 $(IMAGE):$(TAG)"
	@echo "make up / down   - 启动 / 停止 docker compose"
	@echo "make vet         - go vet + gofmt 检查"
	@echo "make version     - 打印当前版本号 ($(VERSION))"

.PHONY: dev-web
dev-web:
	cd web && npm run dev

.PHONY: dev-api
dev-api:
	go run ./cmd/server -root $(ROOT) -addr $(ADDR) -verbose

.PHONY: web
web:
	cd web && npm install && npm run build

.PHONY: build
build: web
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o file-cleaner ./cmd/server

.PHONY: version
version:
	@echo $(VERSION)

.PHONY: image
image:
	docker build --build-arg VERSION=$(VERSION) -t $(IMAGE):$(TAG) .

.PHONY: up
up:
	docker compose up -d

.PHONY: down
down:
	docker compose down

.PHONY: vet
vet:
	gofmt -l cmd internal web
	go vet ./...
