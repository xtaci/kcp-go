# lib

A collection of reusable Go libraries for systems programming, networking, and protocol implementation.

This repository contains independent packages designed to complement the Go standard library. The primary goals are simplicity, portability, minimal dependencies, and reusable APIs.

## Design Goals

- Standard library–first design
- Minimal external dependencies
- Cross-platform support where practical
- Low-allocation and zero-copy implementations where appropriate
- Small, focused, and composable packages
- Reusable infrastructure without application-specific logic

## Documentation

Package documentation is available on [pkg.go.dev](https://pkg.go.dev/gitlab.com/CoiaPrant/lib), or simply browse the source code.

## Stability

The API is intended to remain reasonably stable whenever practical. However, compatibility is **not** guaranteed, including exported APIs and ABI. Breaking changes may be introduced as the library evolves.

## Installation

```bash
go get gitlab.com/CoiaPrant/lib
```

## License

Unless otherwise specified, this repository is licensed under the BSD 3-Clause License. See the `LICENSE` file for details.

Some source files may include bundled or derived code from third-party projects. Such files retain their original license headers where required. If a source file does not contain an explicit license header, refer to the `LICENSE` file in the corresponding bundled upstream source (if present). Otherwise, the repository's top-level `LICENSE` applies.

Known third-party licenses used within this repository include:

- BSD 3-Clause
- MIT