# x-phone

Android cloud phone console. The control plane is Go, node tools are Python, and the operator UI is React. Android runs as Redroid on a Linux host; this repository does not vendor Damru.

## Ports

These are the host ports reserved for x-phone, all inside 10000–20000:

| Port | Service |
| --- | --- |
| 18110 | Go API and WebSocket |
| 18111 | React dev server |
| 18120–18219 | ADB published from each Android container |
| 18220–18221 | Redroid spike containers |

MySQL, Redis, and MinIO are the containers already running on this machine (`mysql-prod`, `redis-prod`, `minio-prod`). x-phone does not start a second copy. Isolation is a separate MySQL database `xphone`, Redis logical database `1`, and MinIO bucket `xphone`.

## Run locally

The API and UI come up together:

```sh
make dev
```

`make dev` creates the `xphone` database on the existing MySQL if it is missing, then listens on http://127.0.0.1:18110. The UI is http://localhost:18111. Login is `admin` / `admin123`.

To start only the API: `make run-server`. To start only the UI: `make web-dev`.

```sh
cd web && npm install && npm run dev
```

Register a host in the UI, then start its agent:

```sh
go run ./cmd/agent -token "$XPHONE_AGENT_TOKEN" -simulate
```

`-simulate` completes lifecycle tasks without Docker so the console can be exercised on macOS. On Ubuntu 24.04 with Docker and binderfs, omit `-simulate`. The agent then calls `python3 -m phone`.

## Node tools

```sh
cd tools/phone
python3 -m venv .venv && . .venv/bin/activate
pip install pytest websockets
PYTHONPATH=. python -m phone spike --profile ../../profiles/pixel_8.json
```

`deploy/spike/redroid-spike.sh` runs the same probe with `--execute` and is intended for an Ubuntu 24.04 host. It starts two Redroid containers, checks `sys.boot_completed` and `ro.product.model`, then removes them.

## Tests

```sh
go test ./...
cd tools/phone && python -m pytest
```

MySQL, Redis, and MinIO stay in the containers that are already running. `make compose-up` only creates database `xphone` inside `mysql-prod`.

## Make targets

```sh
make help          # list every target
make dev           # API on :18110 and UI on :18111
make build         # bin/xphone-server and bin/xphone-agent
make test          # go test ./...
make node-test     # tools/phone pytest
make compose-up    # create database xphone on the existing MySQL
```

`.vscode/launch.json` ships two Go debug configs: `x-phone: server` and `x-phone: agent (simulate)`. The agent config reads `XPHONE_AGENT_TOKEN` from the environment.
