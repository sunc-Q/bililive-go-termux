#!/data/data/com.termux/files/usr/bin/sh
# bililive-go Termux 启动/停止脚本（G50）
# 用法: ./start.sh start|stop|status|log
DIR=$(cd "$(dirname "$0")" && pwd)
cd "$DIR" || exit 1
PID_FILE="$DIR/bililive.pid"

is_running() {
  [ -f "$PID_FILE" ] && kill -0 "$(cat "$PID_FILE")" 2>/dev/null
}

case "$1" in
  stop)
    if is_running; then
      kill "$(cat "$PID_FILE")" 2>/dev/null
      sleep 2
      kill -0 "$(cat "$PID_FILE")" 2>/dev/null && kill -9 "$(cat "$PID_FILE")"
      rm -f "$PID_FILE"
      echo "stopped"
    else
      echo "not running"
    fi
    termux-wake-unlock 2>/dev/null
    ;;
  status)
    if is_running; then
      echo "running (pid $(cat "$PID_FILE"))"
      curl -s -o /dev/null -w "web: %{http_code}\n" --max-time 5 http://127.0.0.1:22290/api/info
    else
      echo "not running"
    fi
    ;;
  log)
    tail -n "${2:-50}" "$DIR/run.log"
    ;;
  start|*)
    if is_running; then
      echo "already running (pid $(cat "$PID_FILE"))"
      exit 0
    fi
    termux-wake-lock 2>/dev/null || echo "[warn] termux-wake-lock 不可用（建议 pkg install termux-api）"
    nohup ./bililive-go -c config.yml >> "$DIR/run.log" 2>&1 &
    echo $! > "$PID_FILE"
    sleep 2
    if is_running; then
      echo "started (pid $(cat "$PID_FILE"))"
      curl -s --max-time 5 http://127.0.0.1:22290/api/info || echo "(web 尚未就绪，稍后 curl http://127.0.0.1:22290/api/info 查看)"
    else
      echo "[FAIL] 进程启动后即退出，最后日志："
      tail -n 30 "$DIR/run.log"
      exit 1
    fi
    ;;
esac
