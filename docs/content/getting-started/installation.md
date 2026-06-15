---
title: "Installation"
description: "Install kegg from a release, with go install, or from source."
weight: 20
---

## Prebuilt binaries

Every [release](https://github.com/tamnd/kegg-cli/releases) carries archives for Linux, macOS,
and Windows on amd64 and arm64, plus deb, rpm, and apk packages for Linux.
Download, unpack, put `kegg` on your `PATH`, done. The `checksums.txt`
on each release is signed with keyless [cosign](https://docs.sigstore.dev/) if
you want to verify before running.

## With Go

```bash
go install github.com/tamnd/kegg-cli/cmd/kegg@latest
```

That puts `kegg` in `$(go env GOPATH)/bin`, which is `~/go/bin` unless
you moved it. Make sure that directory is on your `PATH`.

## From source

```bash
git clone https://github.com/tamnd/kegg-cli
cd kegg-cli
make build        # produces ./bin/kegg
./bin/kegg version
```

## Container image

```bash
docker run --rm ghcr.io/tamnd/kegg:latest --help
```

## Checking the install

```bash
kegg version
```

prints the version and exits.
