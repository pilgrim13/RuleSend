# Third-party notices

RuleSend is an original MIT-licensed project. The following modules are linked into the current executable, based on `go list -deps` for `cmd/rulesend`. Their original license texts and accompanying top-level notices are preserved in `licenses/`. Optional development tools may have additional dependencies. No Syncthing source is included.

| Module | Version | Original notices |
| --- | --- | --- |
| `github.com/dustin/go-humanize` | `v1.0.1` | [LICENSE](licenses/github.com__dustin__go-humanize/LICENSE) |
| `github.com/google/uuid` | `v1.6.0` | [LICENSE](licenses/github.com__google__uuid/LICENSE) |
| `github.com/mattn/go-isatty` | `v0.0.24` | [LICENSE](licenses/github.com__mattn__go-isatty/LICENSE) |
| `github.com/ncruces/go-strftime` | `v1.0.0` | [LICENSE](licenses/github.com__ncruces__go-strftime/LICENSE) |
| `github.com/remyoudompheng/bigfft` | `v0.0.0-20230129092748-24d4a6f8daec` | [LICENSE](licenses/github.com__remyoudompheng__bigfft/LICENSE) |
| `golang.org/x/sys` | `v0.47.0` | [LICENSE](licenses/golang.org__x__sys/LICENSE) |
| `modernc.org/libc` | `v1.74.4` | [LICENSE](licenses/modernc.org__libc/LICENSE), [LICENSE-3RD-PARTY.md](licenses/modernc.org__libc/LICENSE-3RD-PARTY.md) |
| `modernc.org/mathutil` | `v1.7.1` | [LICENSE](licenses/modernc.org__mathutil/LICENSE) |
| `modernc.org/memory` | `v1.11.0` | [LICENSE](licenses/modernc.org__memory/LICENSE), [LICENSE-MMAP-GO](licenses/modernc.org__memory/LICENSE-MMAP-GO), [LICENSE-LOGO](licenses/modernc.org__memory/LICENSE-LOGO), [LICENSE-GO](licenses/modernc.org__memory/LICENSE-GO) |
| `modernc.org/sqlite` | `v1.57.0` | [LICENSE](licenses/modernc.org__sqlite/LICENSE), [LICENSE-SQLITE](licenses/modernc.org__sqlite/LICENSE-SQLITE), [LICENSE-SQLITE_VEC](licenses/modernc.org__sqlite/LICENSE-SQLITE_VEC) |

The Go standard library/runtime is covered by the [Go license](licenses/go/LICENSE). SQLite itself is dedicated to the public domain; its Go translation and related libraries have their own notices above. Recheck this inventory when upgrading dependencies or changing build targets. The release workflow includes these notices in every archive.
