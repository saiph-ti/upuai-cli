#!/bin/sh
# Instala o CLI da Upuai em Linux e macOS.
#
#   curl -fsSL https://raw.githubusercontent.com/saiph-ti/upuai-cli/main/install.sh | sh
#
# Variáveis opcionais:
#   UPUAI_VERSION      versão a instalar (ex.: 0.21.1 ou v0.21.1). Padrão: a mais recente.
#   UPUAI_INSTALL_DIR  diretório de destino. Padrão: /usr/local/bin se gravável,
#                      senão $HOME/.local/bin.
#
# O nome dos arquivos segue o name_template de .goreleaser.yaml; o binário só é
# instalado depois de conferir o SHA-256 contra o checksums.txt do release.
# Windows: use o Scoop (ver README).

set -eu

REPO="saiph-ti/upuai-cli"

fail() {
  printf 'upuai install: %s\n' "$1" >&2
  exit 1
}

command -v curl >/dev/null 2>&1 || fail "curl is required"
command -v tar >/dev/null 2>&1 || fail "tar is required"

case "$(uname -s)" in
  Linux) os="linux" ;;
  Darwin) os="darwin" ;;
  *) fail "unsupported OS $(uname -s) — on Windows use Scoop" ;;
esac

case "$(uname -m)" in
  x86_64 | amd64) arch="x86_64" ;;
  arm64 | aarch64) arch="arm64" ;;
  *) fail "unsupported architecture $(uname -m)" ;;
esac

if [ -n "${UPUAI_VERSION:-}" ]; then
  tag="v${UPUAI_VERSION#v}"
else
  # O redirect de /releases/latest traz a tag sem passar pela API do GitHub, que
  # limita chamadas anônimas por IP — runners de CI compartilham IP.
  latest_url="$(curl -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/${REPO}/releases/latest")" ||
    fail "could not resolve the latest release"
  tag="${latest_url##*/}"
  case "$tag" in
    v[0-9]*) ;;
    *) fail "could not resolve the latest release (got '${tag}')" ;;
  esac
fi
version="${tag#v}"

asset="upuai_${version}_${os}_${arch}.tar.gz"
base_url="https://github.com/${REPO}/releases/download/${tag}"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT INT TERM

printf 'Downloading upuai %s (%s/%s)...\n' "$version" "$os" "$arch"
curl -fsSL -o "$tmp/$asset" "$base_url/$asset" || fail "download failed: $base_url/$asset"
curl -fsSL -o "$tmp/checksums.txt" "$base_url/checksums.txt" || fail "download failed: $base_url/checksums.txt"

expected="$(awk -v name="$asset" '$2 == name { print $1 }' "$tmp/checksums.txt")"
[ -n "$expected" ] || fail "no checksum for $asset in checksums.txt"

if command -v sha256sum >/dev/null 2>&1; then
  actual="$(sha256sum "$tmp/$asset" | awk '{ print $1 }')"
elif command -v shasum >/dev/null 2>&1; then
  actual="$(shasum -a 256 "$tmp/$asset" | awk '{ print $1 }')"
else
  fail "sha256sum or shasum is required to verify the download"
fi
[ "$expected" = "$actual" ] || fail "checksum mismatch for $asset (expected $expected, got $actual)"

tar -xzf "$tmp/$asset" -C "$tmp" upuai || fail "archive does not contain the upuai binary"

if [ -n "${UPUAI_INSTALL_DIR:-}" ]; then
  install_dir="$UPUAI_INSTALL_DIR"
elif [ -w /usr/local/bin ]; then
  install_dir="/usr/local/bin"
else
  install_dir="$HOME/.local/bin"
fi
mkdir -p "$install_dir" || fail "cannot create $install_dir"
[ -w "$install_dir" ] || fail "$install_dir is not writable — set UPUAI_INSTALL_DIR"

# Grava ao lado e renomeia: um upuai em uso nunca fica pela metade.
cp "$tmp/upuai" "$install_dir/.upuai.new"
chmod 0755 "$install_dir/.upuai.new"
mv -f "$install_dir/.upuai.new" "$install_dir/upuai"

printf 'Installed %s\n' "$("$install_dir/upuai" version)"

case ":${PATH}:" in
  *":${install_dir}:"*) ;;
  *)
    if [ -n "${GITHUB_PATH:-}" ]; then
      printf '%s\n' "$install_dir" >>"$GITHUB_PATH"
      printf 'Added %s to GITHUB_PATH for the next steps.\n' "$install_dir"
    else
      # $PATH fica literal de propósito: é a linha que o usuário cola no shell.
      # shellcheck disable=SC2016
      printf 'Add %s to your PATH:\n  export PATH="%s:$PATH"\n' "$install_dir" "$install_dir"
    fi
    ;;
esac
