#!/usr/bin/env python3
"""ts-proxy Android GUI — Web control panel for ts-proxy on Termux."""

import json
import os
import signal
import subprocess
import threading
import time
from datetime import datetime
from pathlib import Path

from flask import Flask, jsonify, render_template_string, request

app = Flask(__name__)

BASE_DIR = Path(__file__).resolve().parent.parent
BINARY_PATH = BASE_DIR / "ts-proxy"
CONFIG_PATH = BASE_DIR / "gui" / "config.json"

state = {
    "process": None,
    "logs": [],
    "max_logs": 500,
    "start_time": None,
    "lock": threading.Lock(),
}


def load_config():
    if CONFIG_PATH.exists():
        return json.loads(CONFIG_PATH.read_text())
    return {
        "hostname": "phone",
        "serve_socks": "127.0.0.1:1080",
        "serve_outaddr": "",
        "tailnet_socks": "",
        "fwd_socks": "",
        "tcp_fwd": "",
        "udp_fwd": "",
        "tsnet_dir": str(BASE_DIR / "tsnet-data"),
        "debug": False,
    }


def save_config(cfg):
    CONFIG_PATH.write_text(json.dumps(cfg, indent=2, ensure_ascii=False))


def build_command(cfg):
    cmd = [str(BINARY_PATH)]
    if cfg.get("hostname"):
        cmd.extend(["-hostname", cfg["hostname"]])
    if cfg.get("tsnet_dir"):
        cmd.extend(["-tsnet-dir", cfg["tsnet_dir"]])
    if cfg.get("debug"):
        cmd.append("-debug")

    socks_addr = cfg.get("serve_socks", "").strip()
    tailnet_addr = cfg.get("tailnet_socks", "").strip()
    fwd_addr = cfg.get("fwd_socks", "").strip()
    outaddr = cfg.get("serve_outaddr", "").strip()

    if socks_addr and tailnet_addr:
        val = socks_addr
        if outaddr:
            val += "," + outaddr
        cmd.extend(["-dual-socks", val])
    elif socks_addr:
        val = socks_addr
        if outaddr:
            val += "," + outaddr
        cmd.extend(["-serve-socks", val])
    elif tailnet_addr:
        cmd.extend(["-tailnet-socks", tailnet_addr])

    if fwd_addr:
        cmd.extend(["-fwd-socks", fwd_addr])

    tcp = cfg.get("tcp_fwd", "").strip()
    udp = cfg.get("udp_fwd", "").strip()
    for rule in tcp.split(";"):
        rule = rule.strip()
        if rule:
            cmd.extend(["-tcp", rule])
    for rule in udp.split(";"):
        rule = rule.strip()
        if rule:
            cmd.extend(["-udp", rule])

    return cmd


def stream_reader(pipe, prefix=""):
    try:
        for line in iter(pipe.readline, b""):
            text = line.decode("utf-8", errors="replace").rstrip()
            ts = datetime.now().strftime("%H:%M:%S")
            with state["lock"]:
                state["logs"].append(f"[{ts}] {prefix}{text}")
                if len(state["logs"]) > state["max_logs"]:
                    state["logs"] = state["logs"][-state["max_logs"]:]
    except Exception:
        pass


def start_process(cfg):
    with state["lock"]:
        if state["process"] and state["process"].poll() is None:
            return False, "Already running"

    cmd = build_command(cfg)
    tsnet_dir = Path(cfg.get("tsnet_dir", str(BASE_DIR / "tsnet-data")))
    tsnet_dir.mkdir(parents=True, exist_ok=True)

    try:
        proc = subprocess.Popen(
            cmd,
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            bufsize=1,
            cwd=str(BASE_DIR),
        )
    except FileNotFoundError:
        return False, f"Binary not found at {BINARY_PATH}"
    except Exception as e:
        return False, str(e)

    with state["lock"]:
        state["process"] = proc
        state["start_time"] = time.time()
        state["logs"].append(f"[{datetime.now().strftime('%H:%M:%S')}] Started: {' '.join(cmd)}")

    t = threading.Thread(target=stream_reader, args=(proc.stdout,), daemon=True)
    t.start()
    return True, "Started"


def stop_process():
    with state["lock"]:
        proc = state["process"]
        if not proc or proc.poll() is not None:
            state["process"] = None
            state["start_time"] = None
            return False, "Not running"

    proc.send_signal(signal.SIGTERM)
    try:
        proc.wait(timeout=5)
    except subprocess.TimeoutExpired:
        proc.kill()
        proc.wait()

    with state["lock"]:
        state["process"] = None
        state["start_time"] = None
        state["logs"].append(f"[{datetime.now().strftime('%H:%M:%S')}] Stopped")
    return True, "Stopped"


# --- Routes ---

@app.route("/")
def index():
    return render_template_string(HTML_TEMPLATE)


@app.route("/api/status")
def api_status():
    with state["lock"]:
        proc = state["process"]
        running = proc is not None and proc.poll() is None
        uptime = None
        if running and state["start_time"]:
            uptime = int(time.time() - state["start_time"])
    return jsonify({
        "running": running,
        "uptime": uptime,
        "binary_exists": BINARY_PATH.exists(),
    })


@app.route("/api/config", methods=["GET", "POST"])
def api_config():
    if request.method == "POST":
        cfg = request.json
        save_config(cfg)
        return jsonify({"ok": True})
    return jsonify(load_config())


@app.route("/api/start", methods=["POST"])
def api_start():
    cfg = load_config()
    ok, msg = start_process(cfg)
    return jsonify({"ok": ok, "msg": msg})


@app.route("/api/stop", methods=["POST"])
def api_stop():
    ok, msg = stop_process()
    return jsonify({"ok": ok, "msg": msg})


@app.route("/api/restart", methods=["POST"])
def api_restart():
    stop_process()
    time.sleep(0.5)
    cfg = load_config()
    ok, msg = start_process(cfg)
    return jsonify({"ok": ok, "msg": msg})


@app.route("/api/logs")
def api_logs():
    with state["lock"]:
        logs = list(state["logs"])
    after = request.args.get("after", type=int, default=0)
    if after < len(logs):
        return jsonify({"logs": logs[after:], "total": len(logs)})
    return jsonify({"logs": [], "total": len(logs)})


@app.route("/api/clear_logs", methods=["POST"])
def api_clear_logs():
    with state["lock"]:
        state["logs"].clear()
    return jsonify({"ok": True})


@app.route("/api/command")
def api_command():
    cfg = load_config()
    cmd = build_command(cfg)
    return jsonify({"command": " ".join(cmd)})


# --- HTML Template ---

HTML_TEMPLATE = r"""<!DOCTYPE html>
<html lang="zh">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width,initial-scale=1,user-scalable=no">
<title>ts-proxy</title>
<style>
*{margin:0;padding:0;box-sizing:border-box}
:root{--bg:#0d1117;--card:#161b22;--border:#30363d;--text:#c9d1d9;--text2:#8b949e;
--accent:#58a6ff;--green:#3fb950;--red:#f85149;--yellow:#d29922;--input-bg:#0d1117}
body{font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,sans-serif;
background:var(--bg);color:var(--text);min-height:100vh;padding:12px;padding-bottom:80px}
.container{max-width:480px;margin:0 auto}
.header{display:flex;align-items:center;justify-content:space-between;padding:8px 0 16px}
.header h1{font-size:20px;font-weight:600}
.status-badge{display:inline-flex;align-items:center;gap:6px;font-size:13px;
padding:4px 10px;border-radius:12px;font-weight:500}
.status-on{background:rgba(63,185,80,.15);color:var(--green)}
.status-off{background:rgba(248,81,73,.15);color:var(--red)}
.dot{width:8px;height:8px;border-radius:50%;background:currentColor}
.card{background:var(--card);border:1px solid var(--border);border-radius:12px;
padding:16px;margin-bottom:12px}
.card h2{font-size:14px;color:var(--text2);margin-bottom:12px;text-transform:uppercase;
letter-spacing:.5px;font-weight:600}
label{display:block;font-size:13px;color:var(--text2);margin-bottom:4px;margin-top:10px}
label:first-child{margin-top:0}
input,textarea,select{width:100%;padding:10px 12px;background:var(--input-bg);
border:1px solid var(--border);border-radius:8px;color:var(--text);font-size:14px;
font-family:inherit;outline:none;transition:border-color .2s}
input:focus,textarea:focus{border-color:var(--accent)}
textarea{resize:vertical;min-height:60px;font-family:"SF Mono",Consolas,monospace;font-size:12px}
.row{display:flex;gap:8px}
.row>*{flex:1}
.btn-row{display:flex;gap:8px;margin-top:12px}
.btn{padding:10px 16px;border:none;border-radius:8px;font-size:14px;font-weight:600;
cursor:pointer;transition:opacity .2s;flex:1;text-align:center}
.btn:active{opacity:.7}
.btn-green{background:var(--green);color:#000}
.btn-red{background:var(--red);color:#fff}
.btn-blue{background:var(--accent);color:#000}
.btn-gray{background:var(--border);color:var(--text)}
.btn:disabled{opacity:.4;cursor:not-allowed}
.log-box{background:#000;border-radius:8px;padding:10px;max-height:300px;overflow-y:auto;
font-family:"SF Mono",Consolas,monospace;font-size:11px;line-height:1.6;color:#8b949e;
white-space:pre-wrap;word-break:break-all}
.info-row{display:flex;justify-content:space-between;font-size:13px;padding:4px 0}
.info-row .label{color:var(--text2)}
.info-row .value{color:var(--text);font-weight:500}
.cmd-preview{background:#000;border-radius:6px;padding:8px 10px;font-family:"SF Mono",Consolas,monospace;
font-size:11px;color:var(--yellow);word-break:break-all;margin-top:8px;line-height:1.5}
.toggle-row{display:flex;align-items:center;justify-content:space-between;padding:6px 0}
.toggle-row label{margin:0}
.switch{position:relative;width:40px;height:24px;flex-shrink:0}
.switch input{opacity:0;width:0;height:0}
.switch .slider{position:absolute;inset:0;background:var(--border);border-radius:12px;cursor:pointer;transition:.2s}
.switch .slider::before{content:"";position:absolute;width:18px;height:18px;left:3px;bottom:3px;
background:#fff;border-radius:50%;transition:.2s}
.switch input:checked+.slider{background:var(--accent)}
.switch input:checked+.slider::before{transform:translateX(16px)}
.help{font-size:11px;color:var(--text2);margin-top:4px}
.tabs{display:flex;gap:0;margin-bottom:12px;border-bottom:1px solid var(--border)}
.tab{padding:8px 16px;font-size:13px;color:var(--text2);cursor:pointer;border-bottom:2px solid transparent;
font-weight:500;transition:.2s}
.tab.active{color:var(--accent);border-bottom-color:var(--accent)}
.tab-content{display:none}
.tab-content.active{display:block}
</style>
</head>
<body>
<div class="container">

<div class="header">
  <h1>ts-proxy</h1>
  <span class="status-badge" id="statusBadge"><span class="dot"></span><span id="statusText">...</span></span>
</div>

<div class="tabs">
  <div class="tab active" data-tab="control">控制</div>
  <div class="tab" data-tab="config">配置</div>
  <div class="tab" data-tab="logs">日志</div>
</div>

<div class="tab-content active" id="tab-control">
  <div class="card">
    <h2>状态</h2>
    <div class="info-row"><span class="label">运行状态</span><span class="value" id="runStatus">-</span></div>
    <div class="info-row"><span class="label">运行时间</span><span class="value" id="uptime">-</span></div>
    <div class="info-row"><span class="label">二进制文件</span><span class="value" id="binaryStatus">-</span></div>
    <div class="btn-row">
      <button class="btn btn-green" id="btnStart" onclick="doAction('start')">启动</button>
      <button class="btn btn-red" id="btnStop" onclick="doAction('stop')">停止</button>
      <button class="btn btn-blue" id="btnRestart" onclick="doAction('restart')">重启</button>
    </div>
  </div>
  <div class="card">
    <h2>命令预览</h2>
    <div class="cmd-preview" id="cmdPreview">-</div>
    <button class="btn btn-gray" style="margin-top:8px" onclick="refreshCmd()">刷新</button>
  </div>
</div>

<div class="tab-content" id="tab-config">
  <div class="card">
    <h2>基本设置</h2>
    <label>设备主机名</label>
    <input id="cfg-hostname" placeholder="phone">
    <label>SOCKS5 监听地址</label>
    <input id="cfg-serve_socks" placeholder="127.0.0.1:1080">
    <label>出站地址配置 (可选)</label>
    <input id="cfg-serve_outaddr" placeholder="tcp4=1.2.3.4">
    <div class="help">格式: tcp4=IP,tcp6=IP,udp4=IP,udp6=IP</div>
  </div>
  <div class="card">
    <h2>高级模式</h2>
    <label>Tailnet SOCKS (留空则用 Serve SOCKS)</label>
    <input id="cfg-tailnet_socks" placeholder="127.0.0.1:1081">
    <div class="help">同时填两项将启用 Dual SOCKS 模式</div>
    <label>转发 SOCKS (远端 Tailscale 地址)</label>
    <input id="cfg-fwd_socks" placeholder="127.0.0.1:1080=mynas.tshost:1080">
  </div>
  <div class="card">
    <h2>端口转发</h2>
    <label>TCP 转发 (分号分隔多条)</label>
    <textarea id="cfg-tcp_fwd" placeholder=":8080=mynas.tshost:80"></textarea>
    <label>UDP 转发 (分号分隔多条)</label>
    <textarea id="cfg-udp_fwd" placeholder=":53=1.1.1.1:53"></textarea>
  </div>
  <div class="card">
    <h2>其他</h2>
    <label>Tailscale 数据目录</label>
    <input id="cfg-tsnet_dir" placeholder="/data/data/com.termux/files/home/ts-proxy/tsnet-data">
    <div class="toggle-row">
      <label>Debug 模式</label>
      <div class="switch"><input type="checkbox" id="cfg-debug"><span class="slider"></span></div>
    </div>
  </div>
  <div class="btn-row">
    <button class="btn btn-green" onclick="saveConfig()">保存配置</button>
    <button class="btn btn-gray" onclick="loadConfig()">重置</button>
  </div>
</div>

<div class="tab-content" id="tab-logs">
  <div class="card" style="padding:8px">
    <div class="log-box" id="logBox"></div>
  </div>
  <div class="btn-row">
    <button class="btn btn-gray" onclick="clearLogs()">清空日志</button>
    <button class="btn btn-gray" onclick="scrollLogs()">跳到最新</button>
  </div>
</div>

</div>

<script>
const $=s=>document.querySelector(s);
let logCount=0,pollTimer=null;
document.querySelectorAll('.tab').forEach(t=>{
  t.addEventListener('click',()=>{
    document.querySelectorAll('.tab').forEach(x=>x.classList.remove('active'));
    document.querySelectorAll('.tab-content').forEach(x=>x.classList.remove('active'));
    t.classList.add('active');
    $(`#tab-${t.dataset.tab}`).classList.add('active');
  });
});
async function api(path,opts){
  const r=await fetch('/api'+path,{headers:{'Content-Type':'application/json'},...opts,
    body:opts?.body?JSON.stringify(opts.body):undefined});
  return r.json();
}
async function refreshStatus(){
  const s=await api('/status');
  const badge=$('#statusBadge'),text=$('#statusText');
  if(s.running){badge.className='status-badge status-on';text.textContent='运行中';
    $('#runStatus').textContent='运行中';$('#uptime').textContent=formatUptime(s.uptime);
    $('#btnStart').disabled=true;$('#btnStop').disabled=false;$('#btnRestart').disabled=false;
  }else{badge.className='status-badge status-off';text.textContent='已停止';
    $('#runStatus').textContent='已停止';$('#uptime').textContent='-';
    $('#btnStart').disabled=false;$('#btnStop').disabled=true;$('#btnRestart').disabled=true;
  }
  $('#binaryStatus').textContent=s.binary_exists?'已就绪':'缺失';
}
function formatUptime(s){if(!s)return'-';const h=Math.floor(s/3600),m=Math.floor(s%3600/60),sec=s%60;
  const p=[];if(h)p.push(h+'h');if(m)p.push(m+'m');p.push(sec+'s');return p.join(' ');}
async function doAction(a){const btn=$(`#btn${a[0].toUpperCase()+a.slice(1)}`);btn.disabled=true;
  await api('/'+a,{method:'POST'});setTimeout(refreshStatus,500);}
async function loadConfig(){const c=await api('/config');
  for(const[k,v]of Object.entries(c)){const el=$(`#cfg-${k}`);if(!el)continue;
    if(el.type==='checkbox')el.checked=!!v;else el.value=v||'';}}
async function saveConfig(){
  const fields=['hostname','serve_socks','serve_outaddr','tailnet_socks','fwd_socks','tcp_fwd','udp_fwd','tsnet_dir'];
  const cfg={};fields.forEach(k=>{cfg[k]=$(`#cfg-${k}`).value;});
  cfg.debug=$('#cfg-debug').checked;await api('/config',{method:'POST',body:cfg});
  refreshCmd();alert('配置已保存');}
async function refreshCmd(){const r=await api('/command');$('#cmdPreview').textContent=r.command;}
async function pollLogs(){const r=await api('/logs?after='+logCount);
  if(r.logs.length){const box=$('#logBox');r.logs.forEach(l=>{const d=document.createElement('div');
    d.style.margin='0';d.textContent=l;box.appendChild(d);});logCount=r.total;
    if(box.scrollHeight-box.scrollTop-box.clientHeight<60)box.scrollTop=box.scrollHeight;}}
function scrollLogs(){$('#logBox').scrollTop=$('#logBox').scrollHeight;}
async function clearLogs(){await api('/clear_logs',{method:'POST'});$('#logBox').innerHTML='';logCount=0;}
loadConfig();refreshStatus();refreshCmd();
pollTimer=setInterval(()=>{refreshStatus();pollLogs();},2000);
</script>
</body>
</html>"""

if __name__ == "__main__":
    port = int(os.environ.get("GUI_PORT", "8088"))
    print(f"ts-proxy GUI: http://127.0.0.1:{port}")
    app.run(host="127.0.0.1", port=port, debug=False)
