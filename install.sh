#!/bin/bash
# brain-context installer for Linux and macOS
# Usage: curl -fsSL https://raw.githubusercontent.com/jinkp/brain-context/master/install.sh | bash

set -e

REPO="jinkp/brain-context"
INSTALL_DIR="${BRAIN_INSTALL_DIR:-$HOME/.local/bin}"
BINARY_NAME="brain"

# ── Colors ────────────────────────────────────────────────────────────────────
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m'

info()    { echo -e "${CYAN}  →${NC} $1"; }
success() { echo -e "${GREEN}  ✅${NC} $1"; }
warn()    { echo -e "${YELLOW}  ⚠️${NC}  $1"; }
error()   { echo -e "${RED}  ❌${NC} $1" >&2; exit 1; }

# ── Detect OS and arch ────────────────────────────────────────────────────────
detect_platform() {
  OS=$(uname -s | tr '[:upper:]' '[:lower:]')
  ARCH=$(uname -m)

  case "$OS" in
    linux)  OS="linux" ;;
    darwin) OS="darwin" ;;
    *)      error "Unsupported OS: $OS. Use install.ps1 on Windows." ;;
  esac

  case "$ARCH" in
    x86_64 | amd64) ARCH="amd64" ;;
    arm64 | aarch64) ARCH="arm64" ;;
    *) error "Unsupported architecture: $ARCH" ;;
  esac

  PLATFORM="${OS}-${ARCH}"
}

# ── Get latest release version ────────────────────────────────────────────────
get_latest_version() {
  if command -v curl &>/dev/null; then
    VERSION=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
      | grep '"tag_name"' | sed 's/.*"tag_name": *"\(.*\)".*/\1/')
  elif command -v wget &>/dev/null; then
    VERSION=$(wget -qO- "https://api.github.com/repos/${REPO}/releases/latest" \
      | grep '"tag_name"' | sed 's/.*"tag_name": *"\(.*\)".*/\1/')
  else
    error "curl or wget is required"
  fi

  if [ -z "$VERSION" ]; then
    warn "Could not detect latest version — using v0.1.0"
    VERSION="v0.1.0"
  fi
}

# ── Download binary ───────────────────────────────────────────────────────────
download_binary() {
  FILENAME="${BINARY_NAME}-${PLATFORM}"
  URL="https://github.com/${REPO}/releases/download/${VERSION}/${FILENAME}"
  TMP_FILE="$(mktemp)"

  info "Downloading brain-context ${VERSION} for ${PLATFORM}..."

  if command -v curl &>/dev/null; then
    curl -fsSL "$URL" -o "$TMP_FILE" || error "Download failed: $URL"
  else
    wget -qO "$TMP_FILE" "$URL" || error "Download failed: $URL"
  fi

  chmod +x "$TMP_FILE"
  mkdir -p "$INSTALL_DIR"
  mv "$TMP_FILE" "${INSTALL_DIR}/${BINARY_NAME}"
}

# ── Add to PATH ───────────────────────────────────────────────────────────────
add_to_path() {
  if echo "$PATH" | grep -q "$INSTALL_DIR"; then
    return 0
  fi

  SHELL_RC=""
  case "$SHELL" in
    */zsh)  SHELL_RC="$HOME/.zshrc" ;;
    */fish) SHELL_RC="$HOME/.config/fish/config.fish" ;;
    *)      SHELL_RC="$HOME/.bashrc" ;;
  esac

  if [ -n "$SHELL_RC" ]; then
    echo "" >> "$SHELL_RC"
    echo "# brain-context" >> "$SHELL_RC"
    echo "export PATH=\"\$PATH:${INSTALL_DIR}\"" >> "$SHELL_RC"
    info "Added ${INSTALL_DIR} to PATH in ${SHELL_RC}"
    info "Run: source ${SHELL_RC}  (or open a new terminal)"
  fi
}

# ── Main ──────────────────────────────────────────────────────────────────────
main() {
  echo ""
  echo "  brain-context installer"
  echo "  ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
  echo ""

  detect_platform
  get_latest_version
  download_binary
  add_to_path

  echo ""
  success "brain-context ${VERSION} installed at ${INSTALL_DIR}/${BINARY_NAME}"
  echo ""
  echo "  Next steps:"
  echo ""
  echo "  1. Login with your team token:"
  echo "     brain login --api https://your-api-url --token brn_tenant_xxx"
  echo ""
  echo "  2. Register your project:"
  echo "     brain register --project my-project --repo ./my-project \\"
  echo "       --embedder openai --model text-embedding-3-large --api-key \$OPENAI_KEY"
  echo ""
  echo "  3. Index your project:"
  echo "     brain index --project my-project"
  echo ""
  echo "  4. Configure your AI client:"
  echo "     brain tui                # Interactive setup"
  echo "     brain tui opencode       # OpenCode"
  echo "     brain tui claude         # Claude Code"
  echo "     brain tui cursor         # Cursor"
  echo "     brain tui all            # All at once"
  echo ""
}

main
