# x-phone 开发入口。风格参考 nebulasforce：变量可覆盖、中文注释、dev 并行拉起子进程。
# 常用：make dev（server + web） / make run-server / make web-dev / make agent / make test

.PHONY: help build build-server build-agent run-server agent dev web-install web-dev web-build \
	free-dev-ports stop fmt vet tidy test test-v test-cover \
	node-venv node-test node-spike compose-up compose-down clean

GOTOOLCHAIN ?= local
export GOTOOLCHAIN

BIN_DIR        ?= bin
SERVER_PKG     ?= ./cmd/server
AGENT_PKG      ?= ./cmd/agent
SERVER_PORT    ?= 18110
HTTP_ADDR      ?= :$(SERVER_PORT)
WEB_DIR        ?= web
WEB_PORT       ?= 18111

ADMIN_USER     ?= admin
ADMIN_PASSWORD ?= admin123
DB_DRIVER      ?= mysql
DATA_DIR       ?= data
DB_DSN         ?= xphone:xphone@tcp(127.0.0.1:3306)/xphone?charset=utf8mb4&parseTime=true&loc=Local
REDIS_ADDR     ?= 127.0.0.1:6379
REDIS_PASSWORD ?= 123456
REDIS_DB       ?= 1
MINIO_ENDPOINT ?= 127.0.0.1:9000
MINIO_ACCESS   ?= minioadmin
MINIO_SECRET   ?= minioadmin@123
MINIO_BUCKET   ?= xphone
MYSQL_ROOT_PASSWORD ?= 123456

AGENT_SERVER   ?= http://127.0.0.1:$(SERVER_PORT)
AGENT_FLAGS    ?= -simulate
TOOLS_DIR      ?= tools/phone
PYTHON         ?= python3
PROFILE        ?= profiles/pixel_8.json
COMPOSE_FILE   ?= deploy/compose/docker-compose.yml

help: ## 列出全部目标
	@grep -hE '^[a-zA-Z][a-zA-Z0-9_-]*:.*?## ' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-18s %s\n", $$1, $$2}'

# 构建 ---------------------------------------------------------------
build: build-server build-agent ## 构建 server 与 agent 到 bin/

build-server: ## 构建控制面 server
	go build -o $(BIN_DIR)/xphone-server $(SERVER_PKG)

build-agent: ## 构建宿主机 agent
	go build -o $(BIN_DIR)/xphone-agent $(AGENT_PKG)

# 运行 ---------------------------------------------------------------
run-server: compose-up ## 启动控制面（:18110，复用已有 MySQL / Redis / MinIO）
	XPHONE_HTTP_ADDR=$(HTTP_ADDR) XPHONE_ADMIN_USER=$(ADMIN_USER) \
	XPHONE_ADMIN_PASSWORD=$(ADMIN_PASSWORD) XPHONE_DB_DRIVER=$(DB_DRIVER) \
	XPHONE_DSN='$(DB_DSN)' XPHONE_DATA_DIR=$(DATA_DIR) \
	XPHONE_REDIS_ADDR=$(REDIS_ADDR) XPHONE_REDIS_PASSWORD=$(REDIS_PASSWORD) XPHONE_REDIS_DB=$(REDIS_DB) \
	XPHONE_MINIO_ENDPOINT=$(MINIO_ENDPOINT) XPHONE_MINIO_ACCESS_KEY=$(MINIO_ACCESS) \
	XPHONE_MINIO_SECRET_KEY='$(MINIO_SECRET)' XPHONE_MINIO_BUCKET=$(MINIO_BUCKET) \
	go run $(SERVER_PKG)

agent: ## 启动 host agent：XPHONE_AGENT_TOKEN=xxx make agent
	@if [ -z "$(XPHONE_AGENT_TOKEN)" ]; then echo "缺少 XPHONE_AGENT_TOKEN：请先在控制台注册主机后复制 token"; exit 1; fi
	go run $(AGENT_PKG) -server $(AGENT_SERVER) -token "$(XPHONE_AGENT_TOKEN)" -pythonpath $(TOOLS_DIR) $(AGENT_FLAGS)

web-install: ## 安装前端依赖
	cd $(WEB_DIR) && npm install

web-dev: ## 启动前端 dev server（:18111，已代理 /api 与 /healthz）
	cd $(WEB_DIR) && npm run dev -- --port $(WEB_PORT)

web-build: ## 构建前端静态产物
	cd $(WEB_DIR) && npm run build

# 释放 server / web 开发端口（有占用才处理）
free-dev-ports:
	@for port in $(SERVER_PORT) $(WEB_PORT); do \
		pids=$$(lsof -tiTCP:$$port -sTCP:LISTEN 2>/dev/null); \
		if [ -n "$$pids" ]; then \
			echo "释放端口 $$port: $$pids"; \
			kill $$pids 2>/dev/null || true; \
			sleep 0.5; \
			pids=$$(lsof -tiTCP:$$port -sTCP:LISTEN 2>/dev/null); \
			if [ -n "$$pids" ]; then kill -9 $$pids 2>/dev/null || true; fi; \
		fi; \
	done

# 并行拉起 Go server 与前端，任一子进程退出则整组退出
dev: free-dev-ports ## 并行启动 server + web
	@echo "dev: server=$(HTTP_ADDR) web=:$(WEB_PORT)"
	@trap 'kill 0' INT TERM EXIT; \
	($(MAKE) run-server HTTP_ADDR=$(HTTP_ADDR); kill 0) & \
	(cd $(WEB_DIR) && npm run dev -- --port $(WEB_PORT); kill 0) & \
	wait

stop: free-dev-ports ## 停止本机开发端口上的监听进程
	@echo "已停止"

# 质量 ---------------------------------------------------------------
fmt: ## gofmt -w .
	gofmt -w .

vet: ## go vet ./...
	go vet ./...

tidy: ## go mod tidy
	go mod tidy

test: ## go test ./...
	go test ./...

test-v: ## go test ./... -v
	go test ./... -v

test-cover: ## go test 覆盖率
	go test ./... -cover

# 节点 Python 工具（tools/phone）---------------------------------------
node-venv: ## 建立虚拟环境并安装 pytest / websockets
	cd $(TOOLS_DIR) && $(PYTHON) -m venv .venv && ./.venv/bin/python -m pip install pytest websockets

node-test: ## 运行 tools/phone 的 pytest
	cd $(TOOLS_DIR) && PYTHONPATH=. $(PYTHON) -m pytest

node-spike: ## 跑 Redroid 探针（默认 profiles/pixel_8.json）
	cd $(TOOLS_DIR) && PYTHONPATH=. $(PYTHON) -m phone spike --profile ../../$(PROFILE)

# 复用已在运行的 mysql-prod / redis-prod / minio-prod，不启动第二套容器。
compose-up: ## 在已有 MySQL 中创建 xphone 库和账号
	MYSQL_ROOT_PASSWORD='$(MYSQL_ROOT_PASSWORD)' sh deploy/compose/ensure-xphone-db.sh

compose-down: ## 不停止共享容器（它们属于已有中间件栈）
	@echo "mysql-prod、redis-prod、minio-prod 保持运行；xphone 只使用独立的库、Redis DB $(REDIS_DB) 和桶 $(MINIO_BUCKET)"

clean: ## 清理构建产物
	rm -rf $(BIN_DIR) $(WEB_DIR)/dist
