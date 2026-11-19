# Third-party notices

cenvkit is licensed under the MIT License — see [LICENSE](LICENSE).

Release binaries are built with `CGO_ENABLED=0` (see `.goreleaser.yaml`), so every
module listed below is compiled into the published `cenvkit` binaries and is
distributed with them. The list is exactly the set of non-cenvkit modules
reachable from `./cmd/cenvkit` (i.e. what `go list -deps` reports), pinned to
the versions in `go.mod` / `go.sum`:

    go list -deps ./cmd/cenvkit | grep -v '^internal' | sort -u   # linked packages
    go list -m all                                                # pinned versions

Re-run those two commands after any dependency change and update the table.

## Modules (28)

| Module | Version | License | Declared in go.mod as |
| --- | --- | --- | --- |
| `github.com/compose-spec/compose-go/v2` | v2.17.0 | Apache-2.0 | direct |
| `github.com/distribution/reference` | v0.5.0 | Apache-2.0 | indirect |
| `github.com/docker/cli` | v29.4.0+incompatible | Apache-2.0 | direct |
| `github.com/docker/go-connections` | v0.4.0 | Apache-2.0 | indirect |
| `github.com/opencontainers/go-digest` | v1.0.0 | Apache-2.0 | indirect |
| `github.com/santhosh-tekuri/jsonschema/v6` | v6.0.1 | Apache-2.0 | indirect |
| `github.com/spf13/cobra` | v1.10.2 | Apache-2.0 | direct |
| `go.yaml.in/yaml/v4` | v4.0.0 | Apache-2.0 | indirect |
| `github.com/spf13/pflag` | v1.0.9 | BSD-3-Clause | indirect |
| `github.com/xhit/go-str2duration/v2` | v2.1.0 | BSD-3-Clause | indirect |
| `golang.org/x/sync` | v0.24.0 | BSD-3-Clause | indirect |
| `golang.org/x/sys` | v0.44.0 | BSD-3-Clause | indirect |
| `golang.org/x/text` | v0.41.0 | BSD-3-Clause | indirect |
| `github.com/aymanbagabas/go-osc52/v2` | v2.0.1 | MIT | indirect |
| `github.com/charmbracelet/colorprofile` | v0.2.3-0.20250311203215-f60798e515dc | MIT | indirect |
| `github.com/charmbracelet/lipgloss` | v1.1.0 | MIT | direct |
| `github.com/charmbracelet/x/ansi` | v0.8.0 | MIT | indirect |
| `github.com/charmbracelet/x/cellbuf` | v0.0.13-0.20250311204145-2c3ea96c31dd | MIT | indirect |
| `github.com/charmbracelet/x/term` | v0.2.1 | MIT | indirect |
| `github.com/go-viper/mapstructure/v2` | v2.4.0 | MIT | indirect |
| `github.com/lucasb-eyer/go-colorful` | v1.2.0 | MIT | indirect |
| `github.com/mattn/go-isatty` | v0.0.20 | MIT | indirect |
| `github.com/mattn/go-runewidth` | v0.0.16 | MIT | indirect |
| `github.com/mattn/go-shellwords` | v1.0.12 | MIT | indirect |
| `github.com/muesli/termenv` | v0.16.0 | MIT | direct |
| `github.com/rivo/uniseg` | v0.4.7 | MIT | indirect |
| `github.com/sirupsen/logrus` | v1.10.1 | MIT | indirect |
| `github.com/xo/terminfo` | v0.0.0-20220910002029-abceb7e1c41e | MIT | indirect |

## NOTICE files (Apache-2.0 §4(d))

The modules below ship a NOTICE file in their distribution; the text is
reproduced verbatim from the Go module cache of the pinned versions.

### github.com/compose-spec/compose-go/v2 v2.17.0

```
The Compose Specification
Copyright 2020 The Compose Specification Authors
```

### github.com/docker/cli v29.4.0+incompatible

```
Docker
Copyright 2012-2017 Docker, Inc.

This product includes software developed at Docker, Inc. (https://www.docker.com).

This product contains software (https://github.com/creack/pty) developed
by Keith Rarick, licensed under the MIT License.

The following is courtesy of our legal counsel:


Use and transfer of Docker may be subject to certain restrictions by the
United States and other governments.
It is your responsibility to ensure that your use and/or transfer does not
violate applicable laws.

For more information, see https://www.bis.doc.gov

See also https://www.apache.org/dev/crypto.html and/or seek legal counsel.
```

### go.yaml.in/yaml/v4 v4.0.0

```
The following files were ported to Go from C files of libyaml, and thus are
still covered by their original MIT license, with the additional copyright
starting in 2011 when the project was ported over:

- internal/libyaml/api.go
- internal/libyaml/emitter.go
- internal/libyaml/parser.go
- internal/libyaml/reader.go
- internal/libyaml/scanner.go
- internal/libyaml/writer.go
- internal/libyaml/yaml.go
- internal/libyaml/yamlprivate.go

Copyright 2006-2010 Kirill Simonov
https://opensource.org/license/mit

All the remaining project files are covered by the Apache license:

Copyright 2011-2019 Canonical Ltd
Copyright 2025 The go-yaml Project Contributors
http://www.apache.org/licenses/LICENSE-2.0
```
