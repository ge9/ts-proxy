#!/data/data/com.termux/files/usr/bin/bash
set -e
echo "=== ts-proxy Android GUI installer ==="

if [ ! -d "/data/data/com.termux" ]; then
  echo "ERROR: This installer requires Termux"
  exit 1
fi

ARCH=$(uname -m)
echo "Detected architecture: $ARCH"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

if [ "$ARCH" = "aarch64" ] || [ "$ARCH" = "arm64" ]; then
  BINARY="ts-proxy"
elif [ "$ARCH" = "x86_64" ]; then
  BINARY="ts-proxy-amd64"
else
  echo "Unsupported architecture: $ARCH"
  exit 1
fi

if [ ! -f "$SCRIPT_DIR/$BINARY" ]; then
  echo "ERROR: Binary $BINARY not found in $SCRIPT_DIR"
  exit 1
fi

echo "Installing dependencies..."
pkg update -y
pkg install -y python python-pip

INSTALL_DIR="$HOME/ts-proxy-gui"
mkdir -p "$INSTALL_DIR/gui" "$INSTALL_DIR/tsnet-data"

cp "$SCRIPT_DIR/$BINARY" "$INSTALL_DIR/ts-proxy"
chmod +x "$INSTALL_DIR/ts-proxy"

if [ -f "$SCRIPT_DIR/gui/app.py" ]; then
  cp "$SCRIPT_DIR/gui/app.py" "$INSTALL_DIR/gui/"
fi

pip3 install flask --quiet

cat > "$INSTALL_DIR/start.sh" << 'EOF'
#!/data/data/com.termux/files/usr/bin/bash
cd "$(dirname "$0")"
termux-wake-lock 2>/dev/null || true
PORT="${GUI_PORT:-8088}"
echo ""
echo "========================================="
echo "  ts-proxy Android GUI"
echo "  http://127.0.0.1:${PORT}"
echo "========================================="
echo ""
python3 gui/app.py
EOF
chmod +x "$INSTALL_DIR/start.sh"

cat > "$INSTALL_DIR/stop.sh" << 'EOF'
#!/data/data/com.termux/files/usr/bin/bash
pkill -f "ts-proxy" 2>/dev/null || true
pkill -f "gui/app.py" 2>/dev/null || true
termux-wake-unlock 2>/dev/null || true
echo "ts-proxy stopped."
EOF
chmod +x "$INSTALL_DIR/stop.sh"

echo ""
echo "========================================="
echo "  Installation complete!"
echo "========================================="
echo ""
echo "  Start:  cd $INSTALL_DIR && bash start.sh"
echo "  Stop:   bash $INSTALL_DIR/stop.sh"
echo "  Open:   http://127.0.0.1:8088"
echo ""
