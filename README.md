# sneat-cli

Text-based User Interface for [Sneat.app](https://sneat.app)

## Install

macOS or Linux, via Homebrew:

```shell
brew install --cask sneat-co/tap/sneat
```

Or build from source with Go:

```shell
go install github.com/sneat-co/sneat-cli/cmd/sneat@latest
```

<!-- dev-approach:v1 -->
## Our approach to development

We build with our own tooling:

- **[SpecScore](https://specscore.md)** — specify requirements as `SpecScore.md` artifacts
- **[SpecStudio](https://specscore.studio)** — author & manage specs across their lifecycle
- **[inGitDB](https://ingitdb.com)** — store structured data in Git where applicable
- **[DALgo](https://dalgo.io)** — data access layer for Go
- **[cover100.dev](https://cover100.dev)** — drive toward 100% test coverage
- **[DataTug](https://datatug.io)** — query & explore data
<!-- /dev-approach -->
