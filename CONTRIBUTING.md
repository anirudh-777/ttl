# Contributing

ttl intentionally stays a single static Go binary backed by SQLite, with an
embedded vanilla-JavaScript web client. Changes should preserve that deployment
model unless an accepted design discussion explicitly changes it.

Before opening a pull request:

```sh
gofmt -w <changed-go-files>
go test ./...
go test -race ./...
go vet ./...
git diff --check
```

Add tests at the lowest layer that owns the invariant and an API or surface test
when behavior is user-visible. Never commit credentials or generated databases.
