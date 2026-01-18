#!/usr/bin/env bash
set -euo pipefail

if ! command -v gh >/dev/null 2>&1; then
  echo "gh CLI is required. Install from https://cli.github.com/" >&2
  exit 1
fi

if [ $# -lt 1 ]; then
  echo "Usage: scripts/publish_release.sh <tag> [notes-file]" >&2
  exit 1
fi

TAG="$1"
NOTES="${2:-RELEASE_NOTES.md}"
VERSION="${TAG#v}"

if [ ! -f "$NOTES" ]; then
  echo "Release notes file '$NOTES' not found" >&2
  exit 1
fi

MAC_APP_PRIMARY="bin/SimonSwitch.app"
MAC_APP_FALLBACKS=("bin/simonswitch.app" "bin/CodeSwitch.app" "bin/codeswitch.app")
MAC_ARCHS=("arm64" "amd64")
MAC_ZIPS=()

package_macos_arch() {
  local arch="$1"
  local staging_app="bin/SimonSwitch-${arch}.app"
  local zip_path="bin/simonswitch-macos-${arch}.zip"

  echo "==> Building macOS ${arch}"
  env ARCH="$arch" wails3 task package ${BUILD_OPTS:-}

  local bundle_path="$MAC_APP_PRIMARY"
  if [ ! -d "$bundle_path" ]; then
    for candidate in "${MAC_APP_FALLBACKS[@]}"; do
      if [ -d "$candidate" ]; then
        bundle_path="$candidate"
        break
      fi
    done
  fi

  if [ ! -d "$bundle_path" ]; then
    echo "Missing asset: $MAC_APP_PRIMARY (or fallbacks: ${MAC_APP_FALLBACKS[*]})" >&2
    exit 1
  fi

  rm -rf "$staging_app"
  mv "$bundle_path" "$staging_app"

  echo "==> Archiving macOS app bundle (${arch})"
  rm -f "$zip_path"
  ditto -c -k --sequesterRsrc --keepParent "$staging_app" "$zip_path"
  rm -rf "$staging_app"

  MAC_ZIPS+=("$zip_path")
}

perl -0pi -e "s/const\\s+AppVersion\\s*=\\s*\"[^\"]*\"/const AppVersion = \"$TAG\"/" version_service.go
perl -pi -e "s/^  version: \"[^\"]*\"/  version: \"$VERSION\"/" build/config.yml
perl -0pi -e "s/\"file_version\": \"[^\"]*\"/\"file_version\": \"$VERSION\"/g; s/\"ProductVersion\": \"[^\"]*\"/\"ProductVersion\": \"$VERSION\"/g" build/windows/info.json
perl -pi -e "s/^version: \"[^\"]*\"/version: \"$VERSION\"/" build/linux/nfpm/nfpm.yaml

wails3 task common:update:build-assets
for arch in "${MAC_ARCHS[@]}"; do
  package_macos_arch "$arch"
done

env ARCH=amd64 wails3 task windows:package ${BUILD_OPTS:-}

# 构建 updater.exe（静默更新辅助程序）
echo "==> Building updater.exe"
wails3 task windows:build:updater ${BUILD_OPTS:-}

# 统一文件名大小写（SimonSwitch.exe）
if [ -f "bin/simonswitch.exe" ] && [ ! -f "bin/SimonSwitch.exe" ]; then
  mv "bin/simonswitch.exe" "bin/SimonSwitch.exe"
fi

# 生成 SHA256 哈希文件
echo "==> Generating SHA256 checksums"
generate_sha256() {
  local file="$1"
  if [ -f "$file" ]; then
    local hash_file="${file}.sha256"
    if command -v sha256sum >/dev/null 2>&1; then
      sha256sum "$file" | awk '{print $1 "  " FILENAME}' FILENAME="$(basename "$file")" > "$hash_file"
    elif command -v shasum >/dev/null 2>&1; then
      shasum -a 256 "$file" | awk '{print $1 "  " FILENAME}' FILENAME="$(basename "$file")" > "$hash_file"
    else
      echo "Warning: no sha256sum or shasum available, skipping hash for $file" >&2
      return 1
    fi
    echo "  hash: $hash_file"
  fi
}

generate_sha256 "bin/SimonSwitch.exe"
generate_sha256 "bin/updater.exe"

ASSETS=(
  "${MAC_ZIPS[@]}"
  "bin/SimonSwitch-amd64-installer.exe"
  "bin/SimonSwitch.exe"
  "bin/SimonSwitch.exe.sha256"
  "bin/updater.exe"
  "bin/updater.exe.sha256"
)

for asset in "${ASSETS[@]}"; do
  [ -e "$asset" ] || { echo "Missing asset: $asset" >&2; exit 1; }
  echo "  asset: $asset"
done

gh release create "$TAG" "${ASSETS[@]}" \
  --title "$TAG" \
  --notes-file "$NOTES"
