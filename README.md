<div align="center">

# Panoxy

**A Linux transparent-proxy gateway deploy/management tool built on the [mihomo](https://github.com/MetaCubeX/mihomo) core**

[![Go](https://img.shields.io/badge/Go-1.23+-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![License](https://img.shields.io/badge/License-Proprietary-red)](LICENSE)
[![Platform](https://img.shields.io/badge/Platform-Linux%20amd64%7Carm64-lightgrey)]()
[![Release](https://img.shields.io/badge/Release-V0.0.1-orange)](../../releases)

Single binary · zero dependencies · transactional deployment · full rollback

</div>

**What it does.** Panoxy turns one Linux box into a transparent proxy gateway for your entire network. Install it on a single machine and every device behind it — phones, laptops, consoles, IoT — is proxied automatically at the network layer (TUN/TPROXY), with no client software and no per-app configuration. It bundles the mihomo core, a web panel, geo/rules data, and a transactional deploy/upgrade flow with full rollback, so standing up and maintaining a gateway is a single command.

> **The name.** *Panoxy* = Greek *πᾶν* (*pan*, "all") + *proxy*: one proxy for *all* of your traffic.

> **Program name.** The default program name is `Panoxy`, and it can be customized to any name at build time (e.g. `myproxy`). Once renamed, the binary name, install path, config path, systemd unit, nft table, environment-variable prefix and man page **all follow** that name. See [Build · Custom program name](#custom-program-name).

---

## ✨ Features

- 🔧 **TUN / TPROXY dual mode** — TUN is stable out of the box (default); TPROXY preserves the client's real source IP with the best kernel forwarding performance
- 🛡️ **DNS hijack = nftables** — a dedicated `inet Panoxy` table; port 53 redirect → mihomo:1053 (no protocol blocked, DoT/DoQ/DoH route normally)
- 🔄 **Self-healing** — `kill -9`/OOM residue is cleaned automatically on `systemctl restart Panoxy`; no manual intervention
- 📡 **Verifiable subscriptions** — prefetch → validate → incremental write → restart → **node count > 0 counts as success**; never a false success
- 🧩 **Config merge** — `merge-conf` does field-level merge of same-named groups (union of proxies/use); base groups are preserved, never deleted
- ⬆️ **Parameterized upgrade** — `--core/--ui/--cli/--check/--core-version`; dry-run validation, automatic rollback on failure
- 📖 **Complete documentation** — every command's `-h/-?/--help` includes examples; `man Panoxy` is generated from the same source as `--help`
- 🔍 **Debug-friendly** — `--verbose` for step-by-step detail; `--debug` for zero-obfuscation of external command/API I/O

## 🚀 Quick start

### Option 1: Single-binary direct install (personal use, recommended)

```bash
# Copy the Panoxy binary to the target machine, then:
sudo Panoxy init '<your-subscription-url>'
```

Nine steps run automatically: precheck → fetch subscription → network probe → download core → download geo/rules → download panel → place assets → deploy service → import subscription. Each step has a progress bar; when a direct connection fails, downloads are proxied through subscription nodes (requires an existing local mihomo core); with no core present it suggests the offline package `deploy`, or manually copying the core to `/opt/Panoxy/bin/mihomo`.

### Option 2: Offline package (for friends)

Download the offline package from [Releases](../../releases) (34 MB, core + geo + UI + rules):

```bash
tar xzf Panoxy-V0.0.1-amd64.tar.gz && cd Panoxy-V0.0.1-amd64
sudo ./Panoxy deploy                 # fully automatic install
sudo Panoxy sub import              # paste the subscription link (no quotes needed)
Panoxy status                        # verify health
```

### Option 3: Pre-install (rootless trial)

```bash
Panoxy try '<subscription-url>'       # sandboxed full-install test, never touches the real system
Panoxy init --dry-run                # read-only rehearsal (environment / download strategy / config render)
```

### Already have your own config?

```bash
sudo Panoxy merge-conf ~/my.yaml    # overlay-merge: same-named groups merge, base groups preserved
sudo Panoxy merge-conf --dry-run ~/my.yaml   # preview the merge decision first
```

## 📐 Architecture

```
                 DNS(53/853)                        Data traffic (non-53)
┌──────────┐  nft redirect → :1053  ┌─┐  routing table → TUN device → mihomo
│ TUN mode │ ─────────────────────► │same│
├──────────┤                        │core│  nft mark 1 + policy routing
│TPROXY mode│  nft redirect → :1053 │  │  + tproxy → :7893 (preserves source IP)
└──────────┘ ─────────────────────► └─┘
```

- Data plane (node/group selection) lives in the **web panel**; transport plane (tun/tproxy) lives in the **CLI**
- mihomo's own outbound traffic is allowed via `routing-mark: 6666` → prevents DNS loop deadlock
- systemd unit has zero `resolvectl`; `fw apply` self-cleans → restart self-heals

### Traffic policy (blocks nothing)

The first goal of transparent proxying is **normal access**; routing is only an optimization of which path traffic takes:

| Protocol | Handling | Notes |
|---|---|---|
| Plain DNS (53) | **hijack** → mihomo | provides domain-level routing (fake-ip) for most devices |
| QUIC/HTTP3 (UDP 443) | **normal routing** | native HTTP/3 experience; SNI is encrypted, so domain rules don't apply (IP routing) |
| DoT (TCP 853) | **normal routing** | encrypted DNS goes through the proxy (preserved for custom devices) |
| DoQ (UDP 853) | **normal routing** | same as above |
| DoH (TCP 443) | **normal routing** | same port as HTTPS, cannot and should not be blocked |

### Direct-connect base services (32 rules, no proxy)

| Category | Ports | Services |
|---|---|---|
| **Remote management** | 22, 23 | SSH/SFTP, Telnet |
| **Remote desktop** | 3389, 5900 | RDP, VNC |
| **VPN/overlay** | 41641, 3478, 51820, 1194, 500, 4500, 1701, 1723 | Tailscale, STUN/TURN, WireGuard, OpenVPN, IPSec (IKE/NAT-T), L2TP, PPTP |
| **VoIP** | 5060, 5061 | SIP, SIPS |
| **Domain auth** | 88, 389, 636, 1812, 1813 | Kerberos, LDAP, LDAPS, RADIUS |
| **Discovery/time** | 5353, 123, 161, 1900 | mDNS, NTP, SNMP, SSDP/UPnP |
| **IoT** | 1883, 8883, 5683 | MQTT, MQTT/TLS, CoAP |
| **Storage/database** | 3260, 3306, 5432, 6379, 27017, 873 | iSCSI, MySQL, PostgreSQL, Redis, MongoDB, Rsync |
| **Tailscale-specific** | 100.100.100.100, 100.64.0.0/10 | MagicDNS, CGNAT subnet |

## 📂 Repository layout

```
Panoxy/
├── src/               Go source (cmd/internal/tests)
├── dist/              release artifacts (binary + offline package, gitignored)
├── build.sh           packaging/distribution script (offline package / subscription bootstrap / leak scan)
├── docs/              extended docs
│   ├── TPROXY.md      complete TPROXY-mode guide
│   ├── MIGRATION.md   bash-version migration steps
│   ├── KNOWN-LIMITATIONS.md
│   └── TROUBLESHOOTING.md
├── legacy/            archived old bash version
├── Makefile           local build/install entry (make)
└── README.md
```

## 🛠️ Build

### Prerequisites

- Go 1.23+ ([install](https://go.dev/dl/))
- No CGO dependency (fully static build)

### Using Makefile (recommended)

```bash
make                                    # build current arch → dist/ (amd64 auto-detects AVX2)
make build                              # same as above (explicit)
make install                            # install CLI → /usr/local/bin/Panoxy (PREFIX/BINDIR customizable)
make build PANOXY_VERSION=V0.0.1        # set a version number
make build PROG=myproxy                 # customize program name (default Panoxy, see "Custom program name")
```

### Using the script

```bash
./build.sh                              # build current arch (default)
./build.sh --arch arm64                 # target arch
./build.sh --arch all                   # both arches
./build.sh --ver V0.0.1                 # set version
```

### Manual build

```bash
cd src

# native arch (amd64)
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOAMD64=v3 \
  go build -trimpath -ldflags "-s -w -X main.version=V0.0.1" \
  -o ../dist/Panoxy-linux-amd64 \
  ./cmd/panixy

# cross-compile ARM64 (no ARM machine needed)
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 \
  go build -trimpath -ldflags "-s -w -X main.version=V0.0.1" \
  -o ../dist/Panoxy-linux-arm64 \
  ./cmd/panixy

# generate checksums
cd ../dist && sha256sum Panoxy-linux-* > sha256sums.txt
```

<details>
<summary>📖 Build flags reference</summary>

| Flag | Effect |
|---|---|
| `CGO_ENABLED=0` | fully static build, no libc dependency, runs on any Linux |
| `-trimpath` | strip build-machine path info (security + size) |
| `-ldflags "-s -w"` | strip symbol table and debug info (size -30%) |
| `-X main.version=X` | inject the version number (shown by `Panoxy --version`) |
| `-X github.com/deadship2003/Panoxy/internal/constants.ProgName=X` | inject the program name (default `Panoxy`; see [Custom program name](#custom-program-name)) |
| `GOAMD64` | amd64 build level: `build.sh` auto-detects AVX2 by default (present → v3, absent → v1); manually override with `GOAMD64=v3`/`v1` |

</details>

### Custom program name

The default program name `Panoxy` is defined in `internal/constants.ProgName`; inject `-X` at build time to rename it — the binary and all runtime artifacts **inherit** the name:

```bash
# rename to myproxy: binary name, install path, config path, systemd unit,
# nft table, env prefix, man page all follow
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
  go build -trimpath \
  -ldflags "-s -w -X main.version=V0.0.1 -X github.com/deadship2003/Panoxy/internal/constants.ProgName=myproxy" \
  -o ../dist/myproxy-linux-amd64 \
  ./cmd/panixy
```

The easy way is to hand it to the build entry:

```bash
make PROG=myproxy                     # Makefile: PROG variable
./build.sh --prog myproxy             # build.sh: --prog flag
PROG=myproxy ./build.sh package       # or env var PROG (packaging works the same)
```

Runtime artifacts follow the renamed program (using `myproxy` as the example):

| Dimension | Default `Panoxy` | Renamed `myproxy` |
|---|---|---|
| Binary / install path | `Panoxy` → `/usr/local/bin/Panoxy` | `myproxy` → `/usr/local/bin/myproxy` |
| Config / root dir | `/etc/Panoxy.yaml` · `/opt/Panoxy` | `/etc/myproxy.yaml` · `/opt/myproxy` |
| systemd unit | `Panoxy.service` etc. | `myproxy.service` etc. |
| nft table | `inet Panoxy` | `inet myproxy` |
| Env prefix | `PANOXY_` | `MYPROXY_` (program name uppercased, `-`→`_`) |
| man page | `Panoxy.1.gz` · `man Panoxy` | `myproxy.1.gz` · `man myproxy` |

> Note: the program name (a build-time variable) and the GitHub repo name (`deadship2003/Panoxy`) are two different things — renaming only affects the binary and runtime artifacts, not the repo or the upgrade source.

> **CPU selection for CLI and core**: the Panoxy CLI auto-detects `GOAMD64` against the current CPU at build time by default (amd64 with AVX2 → `v3`, without → `v1` full compatibility; force with `GOAMD64=v1 ./build.sh`). The mihomo core, on the other hand, is matched at **runtime** by `Panoxy init` / `Panoxy upgrade` / `build.sh package`, which probe the local arch and AVX2 to download a matching core (AVX2 → `v3`, otherwise → standard, falling back to `compatible`); once downloaded/cached the core is not re-probed.

### Verify the build

```bash
file dist/Panoxy-linux-amd64
# ELF 64-bit LSB executable, x86-64, statically linked ✓

dist/Panoxy-linux-amd64 --version
# Panoxy version V0.0.1
```

## 📦 Packaging

### Using build.sh

```bash
./build.sh package                       # current arch (default)
./build.sh package all                    # all target platforms (amd64+arm64)
./build.sh package --arch arm64 --ver V0.0.1
./build.sh package -h                    # help
```

### Script flags / env vars

| Flag / var | Default | Meaning |
|---|---|---|
| `--arch amd64\|arm64\|all` | current platform | target arch |
| `--ver V0.0.1` | git describe | version number |
| `--prog Panoxy` (or `PROG` env) | `Panoxy` | program name (build-time injection; determines binary/package name and runtime paths) |
| `--sub-url URL` | (empty) | download assets through subscription proxy when offline |
| `ASSETS_SRC` | `/opt/Panoxy` | local assets dir (copied if present, not downloaded) |
| `MIHOMO_VERSION` | latest probed at runtime | core version (pin for reproducibility) |
| `PROXY_PORT` | `33999` | subscription bootstrap proxy port |

### Packaging flow (internal steps)

```
[1/5] build ─── inline go build → dist/Panoxy-linux-<arch> (both arches when `all`)
[2/5] assets ── local first (ASSETS_SRC) > direct (15s check) > subscription proxy > gh mirror
                 download: mihomo core + geo×3 + Country.mmdb + HyperADRules + metacubexd UI
[3/5] scan ──── subscription-leak detection (token= etc. → abort; URL never enters the package)
[4/5] assemble ─ Panoxy-V<ver>-<arch>/{Panoxy, README.md, assets/}
[5/5] package ── tar.gz + sha256 → dist/
```

### Manual packaging

<details>
<summary>📖 Expand the full manual packaging steps</summary>

```bash
cd ~/Panoxy
mkdir -p dist

# ===== Step 1: build =====
cd src
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
  go build -trimpath -ldflags "-s -w -X main.version=V0.0.1" \
  -o ../dist/Panoxy-linux-amd64 ./cmd/panixy
cd ..

# ===== Step 2: download assets =====
TMP=$(mktemp -d)
# probe the latest upstream core version at runtime (not hardcoded);
# fall back to the local /opt/Panoxy/bin/mihomo -v when offline
MIHOMO_VER="$(curl -fsSL --connect-timeout 8 https://api.github.com/repos/MetaCubeX/mihomo/releases/latest \
  | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -1)"

# mihomo core (18MB): probe AVX2 to pick v3/standard (same source as build.sh package)
if grep -qw avx2 /proc/cpuinfo; then
  curl -fsSL -o "$TMP/mihomo-linux-amd64-$MIHOMO_VER.gz" \
    "https://github.com/MetaCubeX/mihomo/releases/download/$MIHOMO_VER/mihomo-linux-amd64-v3-$MIHOMO_VER.gz"
else
  curl -fsSL -o "$TMP/mihomo-linux-amd64-$MIHOMO_VER.gz" \
    "https://github.com/MetaCubeX/mihomo/releases/download/$MIHOMO_VER/mihomo-linux-amd64-$MIHOMO_VER.gz"
fi

# geo trio (28MB)
geo="https://github.com/MetaCubeX/meta-rules-dat/releases/download/latest"
curl -fsSL -o $TMP/GeoIP.dat    "$geo/geoip.dat"
curl -fsSL -o $TMP/GeoSite.dat  "$geo/geosite.dat"
curl -fsSL -o $TMP/Country.mmdb "$geo/country.mmdb"

# ad rules
curl -fsSL -o $TMP/HyperADRules-Ads.yaml \
  "https://github.com/Lynricsy/HyperADRules/releases/latest/download/hyper_adrules_ads_clash.yaml"

# metacubexd panel
curl -fsSL -o $TMP/ui.tgz \
  "https://github.com/MetaCubeX/metacubexd/releases/latest/download/compressed-dist.tgz"

# ===== Step 3: assemble the offline package =====
PKG="Panoxy-V0.0.1-amd64"
rm -rf "$PKG"
mkdir -p "$PKG/assets/core" "$PKG/assets/geo" "$PKG/assets/ui/official" "$PKG/assets/rule"

cp dist/Panoxy-linux-amd64 "$PKG/Panoxy"
chmod +x "$PKG/Panoxy"
cp "$TMP/mihomo-linux-amd64-$MIHOMO_VER.gz" "$PKG/assets/core/"
cp $TMP/Geo*.dat $TMP/Country.mmdb "$PKG/assets/geo/"
cp $TMP/HyperADRules-Ads.yaml "$PKG/assets/rule/"
tar xzf $TMP/ui.tgz -C "$PKG/assets/ui/official"
cp README.md "$PKG/"

# ===== Step 4: tar it up =====
tar -czf "dist/$PKG.tar.gz" "$PKG"
(cd dist && sha256sum "$PKG.tar.gz" > "$PKG.tar.gz.sha256")

# ===== Step 5: clean up =====
rm -rf "$PKG" $TMP
echo "artifact: dist/$PKG.tar.gz ($(du -h dist/$PKG.tar.gz | cut -f1))"
```

**Skip downloads when assets already exist locally:**

```bash
# core version taken from the installed core (offline packaging; same local fallback as build.sh package)
MIHOMO_VER="$(/opt/Panoxy/bin/mihomo -v 2>/dev/null | grep -oE 'v[0-9]+\.[0-9]+\.[0-9]+' | head -1)"
gzip -c /opt/Panoxy/bin/mihomo > "$PKG/assets/core/mihomo-linux-amd64-$MIHOMO_VER.gz"
cp /opt/Panoxy/Geo*.dat /opt/Panoxy/Country.mmdb "$PKG/assets/geo/"
cp /opt/Panoxy/rule_provider/HyperADRules-Ads.yaml "$PKG/assets/rule/"
```

</details>

### Final package structure

```
Panoxy-V0.0.1-amd64/
├── Panoxy                                    ← Go binary (9MB)
├── README.md
└── assets/
    ├── core/mihomo-linux-amd64-<version>.gz  ← core (18MB)
    ├── geo/GeoIP.dat GeoSite.dat Country.mmdb
    ├── rule/HyperADRules-Ads.yaml            ← ad rules
    └── ui/official/                          ← metacubexd panel (161 files)
```

**~34 MB total** · the recipient runs `tar xzf` → `sudo ./Panoxy deploy` and installation is done.

### CI auto-packaging

Pushing a `V*` tag triggers GitHub Actions, using the same script as local `build.sh package`:

```bash
git tag V0.0.1 && git push origin V0.0.1
# → CI auto-builds, packages and publishes a Release
```

**Subscription URLs never enter the package**: a pre-packaging scan aborts on patterns like `token=`.

## 📋 Command reference

| Command | Effect |
|---|---|
| `Panoxy try [URL]` | pre-install (rootless sandboxed trial) |
| `Panoxy init/deploy --dry-run` | dry-run mode (rootless) |
| `sudo Panoxy init [URL]` | bare-metal init (nine steps with progress) |
| `sudo Panoxy deploy [URL]` | deploy from the offline package |
| `sudo Panoxy redeploy` | in-place reinstall: force-refresh all program files (config preserved), re-apply firewall and restart |
| `sudo Panoxy merge-conf <yaml>` | overlay-merge a personal config (`--dry-run`/`--rollback`) |
| `Panoxy config [--mode tun\|tproxy] [--write]` | print the default config template (rootless; `--write` writes config.default.yaml) |
| `sudo Panoxy sub import [URL]` | import a subscription (paste mode, no quotes) |
| `sudo Panoxy sub del --name N` | delete a subscription |
| `Panoxy sub list [--json]` | per-subscription status / node count |
| `Panoxy status [-q\|--json\|--detail]` | health overview (`-q` exit code for monitoring) |
| `sudo Panoxy mode [tun\|tproxy]` | view/switch mode |
| `sudo Panoxy upgrade [--core\|--ui\|--cli] [--check]` | parameterized upgrade |
| `sudo Panoxy rollback [vX]` | core rollback |
| `Panoxy check [yaml]` | validate a config |
| `sudo Panoxy apply-conf <yaml>` | apply a config (hot-reload first) |
| `sudo Panoxy uninstall` | uninstall (data preserved) |
| `Panoxy man [command]` | view manual (root page or subcommand page) |
| `sudo Panoxy fw <apply\|teardown\|clean>` | firewall management |

**Global flags**: `--root <dir>` custom install dir · `--verbose` step-by-step detail · `--debug` full transparency

## 🧪 Testing

```bash
make test          # unit tests (YAML editor / firewall-rule text / template -t)
make e2e           # end-to-end tests (real core + fake systemd, ~60s)
make test-all      # everything
make lint          # go vet
```

<details>
<summary>📖 Testing pyramid</summary>

| Layer | Tests what | In Panoxy | Count | Speed |
|---|---|---|---|---|
| Unit | single function | YAML merge / firewall-rule generation / template rendering | ~15 | <1s |
| Integration | component interplay | config through mihomo `-t` | ~5 | 1-2s |
| E2E | full flow | deploy → sub import → status end-to-end | 3 | ~50s |

E2E uses the real compiled binary + real mihomo core + a mock subscription server + fake systemd; business logic is not mocked, so it validates the actual user experience.

</details>

## 📖 More docs

| Doc | Contents |
|---|---|
| [docs/TPROXY.md](docs/TPROXY.md) | complete TPROXY-mode guide (precheck / switch / verify / network topology / troubleshooting) |
| [docs/MIGRATION.md](docs/MIGRATION.md) | migration steps from the bash version |
| [docs/KNOWN-LIMITATIONS.md](docs/KNOWN-LIMITATIONS.md) | known limitations (mihomo limits / DoH / core requirements, etc.) |
| [docs/TROUBLESHOOTING.md](docs/TROUBLESHOOTING.md) | troubleshooting guide |
| `Panoxy man` | manual (after deploy: `man Panoxy` / `man Panoxy-<command>`) |

## 📄 License

Panoxy itself is under a **commercial-rights-reserved** proprietary license: the source is public for audit/evaluation only; commercial use and redistribution require separate authorization, see [LICENSE](LICENSE).

Bundled/downloaded third-party components (mihomo, metacubexd, meta-rules-dat, etc.) each carry their own open-source license, see [THIRD_PARTY_NOTICES](THIRD_PARTY_NOTICES) and [LICENSES/](LICENSES/).

---

<div align="center">
<sub>Built with Go · Powered by <a href="https://github.com/MetaCubeX/mihomo">mihomo</a></sub>
</div>
