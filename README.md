# go-colab

[![CI](https://github.com/thangvv-tech/go-colab/actions/workflows/ci.yml/badge.svg)](https://github.com/thangvv-tech/go-colab/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/thangvv-tech/go-colab/colab.svg)](https://pkg.go.dev/github.com/thangvv-tech/go-colab/colab)

Go client API and CLI for Google Colab runtimes.

`colab` is importable by backend services. `cmd/colab` is a Cobra wrapper over same API.

This project ports Colab CLI behavior and wire contracts to Go. It is an independent Go module: it does not install, call, query releases from, or share versioning with `google-colab-cli` Python package.

> Colab tunnel endpoints are not public stability contracts. Default suite uses HTTP/WebSocket protocol mocks. Run live integration only with explicit credentials and `COLAB_INTEGRATION=1`.

## Requirements

- Go 1.26+
- Google account with Colab access
- Backend services: Application Default Credentials (ADC), Workload Identity, service account, Cloud Run, GCE, or local `gcloud auth application-default login`

## Install

```bash
go get github.com/thangvv-tech/go-colab/colab
go install github.com/thangvv-tech/go-colab/cmd/colab@latest
```

## Backend API

```go
ctx := context.Background()

client, err := colab.NewClient(ctx, colab.NewADCAuth(), colab.ProdEnv)
if err != nil {
    log.Fatal(err)
}

store := colab.NewSessionStore("")
runtimes := colab.NewRuntimeManager(client, store)

session, err := runtimes.Assign(ctx, colab.AssignOptions{
    Name: "training",
    Accelerator: colab.AcceleratorA100,
    HighMemory: true,
})
if err != nil {
    log.Fatal(err)
}
defer runtimes.Unassign(context.WithoutCancel(ctx), session.Endpoint)

result, err := colab.NewExecutor(session, nil).Exec(ctx, `print("hello from Colab")`)
if err != nil || result.Error != "" {
    log.Fatal(err, result.Error)
}
log.Print(result.Stdout)
```

For a caller-managed authenticated client:

```go
client := colab.NewClientFromHTTP(authenticatedHTTPClient, colab.ProdEnv)
```

## Core API

| API | Purpose |
| --- | --- |
| `NewADCAuth`, `NewOAuth2Auth` | Backend ADC and installed-app OAuth2 auth |
| `NewClient`, `NewClientFromHTTP` | Authenticated Colab HTTP client |
| `NewRuntimeManager` | Allocate/list/release runtimes |
| `RuntimeManager.Status`, `RuntimeURL`, `StopAll` | Runtime lifecycle metadata |
| `Executor.Exec`, `ExecuteNotebook`, `Restart` | Python, notebook, and kernel execution |
| `NewContentsClient` | Jupyter Contents REST upload/download/list/delete |
| `NewFileManager` | Contents-backed remote file manager |
| `RuntimeManager.Run` | Ephemeral script job with cleanup and exit status |
| `NewHistoryStore` | JSONL history and JSONL/IPYNB/Markdown/text export |
| `NewAutomation` | Runtime GCP auth, Drive mount, package install |
| `SSHBridge` | Raw Colab SSH WebSocket proxy bridge |
| `Client.GetUsage` | Compute-unit balance and consumption rate |
| `CheckLatestVersion`, `PaymentURL` | Configured go-colab release metadata and subscription URL |

## CLI authentication

CLI defaults to OAuth2 installed-app login, matching Colab's user-oriented workflow. First command opens an authorization URL and caches its token locally with owner-only permissions:

```bash
colab sessions
```

No `--oauth-config` flag is required: public client configuration is bundled in CLI. Override it only with your own OAuth client:

```bash
colab --oauth-config ./client_secret.json sessions
```

Use ADC explicitly for local automation or Google-hosted backend identity:

```bash
gcloud auth application-default login \
  --scopes=openid,https://www.googleapis.com/auth/cloud-platform,https://www.googleapis.com/auth/userinfo.profile,https://www.googleapis.com/auth/userinfo.email,https://www.googleapis.com/auth/colaboratory,https://www.googleapis.com/auth/drive.file

colab --auth adc sessions
```

Cloud Run/GCE/GKE backend workloads should use attached Workload Identity or service accounts, not interactive `gcloud` login.

## CLI

```bash
colab new --session trainer --gpu A100 --high-mem
colab status --session trainer
colab url --session trainer --open
colab exec --session trainer "print('hello')"
colab exec --session trainer --file report.ipynb
colab run --gpu T4 --env MODE=train train.py -- --epochs 5
colab files upload --session trainer train.py /content/train.py
colab files download --session trainer /content/model.bin ./model.bin
colab edit --session trainer /content/train.py
colab install --session trainer -r requirements.txt
colab drivemount --session trainer
colab auth --session trainer
colab log --session trainer --output execution.ipynb
colab ssh --session trainer
colab restart-kernel --session trainer
colab stop --session trainer
```

Commands:

```text
new                 allocate CPU/GPU/TPU runtime
sessions            list runtime assignments
status              inspect saved runtime metadata
url                 print/open runtime browser URL
restart-kernel      restart active Jupyter kernel
stop [--all]        release one/all local runtime sessions
exec                execute Python or .ipynb
repl                interactive or piped Python session
run                 allocate, execute script, release unless --keep
ls/upload/download/rm
files ls/upload/download/rm  grouped aliases for same remote file operations
edit                download, invoke $EDITOR, upload when changed
auth                authenticate runtime for Google Cloud
drivemount          mount Google Drive
install             install packages with uv then pip fallback
log                 inspect/export JSONL/IPYNB/Markdown/text history
ssh                 OpenSSH or raw --proxy-mode Colab bridge
usage               show compute-unit usage
pay [--open]        subscription URL
version             CLI version
update              query optional go-colab release endpoint
```

## Releases and updates

`go-colab` does not query PyPI or any Python package registry. Until this repository has its own tagged release feed, upgrade directly from Go module source:

```bash
go install github.com/thangvv-tech/go-colab/cmd/colab@latest
```

`colab update` prints this command by default. Supply `--release-endpoint` only after operating a go-colab-owned JSON endpoint returning `{"version":"x.y.z"}`.

## API modes

`RuntimeManager.Mode` defaults to `APIModeTunnel`, needed for allocation and release. `APIModePublic` lists `/v1beta/runtimes`; it intentionally blocks allocation/release because public API has no matching lifecycle operations. CLI exposes `--public-api` and `--no-public-api`; these flags are mutually exclusive.

## Quality

CI runs formatting, unit tests, race detection, static analysis, and CLI build verification. Live Colab validation requires an authenticated account and may consume compute units; follow the release maintainer process before running it.

See [CONTRIBUTING.md](CONTRIBUTING.md) for contributor requirements.

## Release compatibility

The library and CLI are tested against live CPU, T4 GPU standard, and V5E1 TPU runtime allocation. High-memory accelerator availability and Public Runtime API access depend on account entitlement and regional capacity.

`go-colab` handles Colab's tunnel API wire protocol. Colab does not provide a public stability contract for those internal endpoints, so releases should include live smoke validation.

## Security

Runtime proxy tokens, OAuth codes, Drive consent URLs, session state, and SSH private keys are secrets. CLI display output redacts runtime token and proxy URL fields; do not log or share local state files.

## License

[Apache-2.0](LICENSE).

## Support and contributing

Read [CONTRIBUTING.md](CONTRIBUTING.md) before opening a pull request. Report vulnerabilities privately per [SECURITY.md](SECURITY.md). Release notes live in [CHANGELOG.md](CHANGELOG.md).

