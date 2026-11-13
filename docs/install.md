# Install

Homebrew and `go install` are in the [README](../README.md#install). The steps below are for a binary from the [latest release](https://github.com/nilox94/tscatalog/releases/latest).

## Release binaries

Two kinds of asset are published for each OS and architecture.

| Asset | What it is |
| --- | --- |
| `tscatalog-<os>-<arch>` and `cat9s-<os>-<arch>` | The programs, uncompressed. Download these. |
| `tscatalog_<version>_<os>_<arch>.tar.gz` | Both programs. The Homebrew cask installs this archive. |

| | amd64 | arm64 |
| --- | --- | --- |
| Linux | `tscatalog-linux-amd64`, `cat9s-linux-amd64` | `tscatalog-linux-arm64`, `cat9s-linux-arm64` |
| macOS | `tscatalog-darwin-amd64`, `cat9s-darwin-amd64` | `tscatalog-darwin-arm64`, `cat9s-darwin-arm64` |

Check `SHA256SUMS` on that release before renaming. The checksum file lists the asset names.

Linux:

```
sha256sum -c SHA256SUMS
```

macOS:

```
shasum -a 256 -c SHA256SUMS
```

The binaries are not notarized and have no Developer ID. macOS quarantines the download. Clear the attribute on the downloaded names, then rename them and put them on `PATH`. This is the arm64 Mac pair. Other rows in the table use their own names. Linux skips `xattr`.

```
xattr -d com.apple.quarantine tscatalog-darwin-arm64 cat9s-darwin-arm64
mv tscatalog-darwin-arm64 tscatalog
mv cat9s-darwin-arm64 cat9s
chmod +x tscatalog cat9s
```

## Other systems

Windows is not a supported target. `detach: true` fails there with `launch detach is only supported on unix`.

`autostart install` and `autostart uninstall` are macOS only. On Linux, run `tscatalog autostart` from a user service. [Autostart](catalog.md#autostart).
