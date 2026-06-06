import * as vscode from "vscode";
import * as cp from "child_process";
import * as path from "path";
import * as http from "http";

// ---------------------------------------------------------------------------
// Types — mirrors Go storage.EventRow and storage.Stats
// ---------------------------------------------------------------------------

interface EventRow {
  eventId: string;
  createdAt: string;
  decision: "ALLOW" | "BLOCK";
  source: string;
  reason: string;
  latencyMs: number;
  modelName: string;
  backend: string;
  upstreamCalled: boolean;
  path: string;
  promptText?: string | null;
  matchedSnippet?: string | null;
}

interface EventPage {
  total: number;
  events: EventRow[];
}

interface Stats {
  total: number;
  blocked: number;
  allowed: number;
  blockRate: number;
  avgLatencyMs: number;
  p95LatencyMs: number;
  bySource: Record<string, number>;
}

interface AdminMeta {
  model: string;
  backend: string;
  listenAddr: string;
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function cfg<T>(key: string, fallback: T): T {
  return vscode.workspace.getConfiguration("promptgate").get<T>(key, fallback);
}

function adminBase(): string {
  return `http://${cfg("proxyAddr", "127.0.0.1:8787")}`;
}

function httpGet<T>(url: string): Promise<T> {
  return new Promise((resolve, reject) => {
    const u = new URL(url);
    const req = http.get(
      { host: u.hostname, port: Number(u.port) || 80, path: u.pathname + u.search },
      (res) => {
        let data = "";
        res.on("data", (c: string) => (data += c));
        res.on("end", () => {
          try { resolve(JSON.parse(data)); }
          catch (e) { reject(e); }
        });
      }
    );
    req.on("error", reject);
    req.setTimeout(3000, () => { req.destroy(); reject(new Error("timeout")); });
  });
}

// ---------------------------------------------------------------------------
// Activity Log Tree
// ---------------------------------------------------------------------------

class EventItem extends vscode.TreeItem {
  constructor(public readonly row: EventRow) {
    const icon = row.decision === "BLOCK" ? "$(error)" : "$(pass)";
    const ts = row.createdAt.slice(11, 19); // HH:mm:ss
    const preview = row.reason?.slice(0, 50) ?? row.source;
    super(`${icon} [${ts}] ${preview}`, vscode.TreeItemCollapsibleState.Collapsed);
    this.description = `${row.decision} · ${row.latencyMs}ms`;
    this.tooltip = new vscode.MarkdownString(
      `**${row.decision}** — ${row.source}\n\n${row.reason}\n\n*${row.createdAt}*`
    );
    this.contextValue = "guardEvent";
  }
}

class EventDetailItem extends vscode.TreeItem {
  constructor(label: string, detail: string) {
    super(label, vscode.TreeItemCollapsibleState.None);
    this.description = detail;
    this.iconPath = new vscode.ThemeIcon("info");
  }
}

class ActivityLogProvider implements vscode.TreeDataProvider<vscode.TreeItem> {
  private _onChange = new vscode.EventEmitter<void>();
  readonly onDidChangeTreeData = this._onChange.event;
  private events: EventRow[] = [];
  private total = 0;

  update(page: EventPage) {
    this.events = page.events;
    this.total = page.total;
    this._onChange.fire();
  }

  getTreeItem(e: vscode.TreeItem): vscode.TreeItem { return e; }

  getChildren(element?: vscode.TreeItem): vscode.TreeItem[] {
    if (!element) {
      return this.events.map((r) => new EventItem(r));
    }
    if (element instanceof EventItem) {
      const r = element.row;
      const items: vscode.TreeItem[] = [
        new EventDetailItem("判定", r.decision),
        new EventDetailItem("ソース", r.source),
        new EventDetailItem("理由", r.reason),
        new EventDetailItem("レイテンシ", `${r.latencyMs} ms`),
        new EventDetailItem("モデル", r.modelName),
        new EventDetailItem("バックエンド", r.backend),
        new EventDetailItem("上流呼び出し", r.upstreamCalled ? "Yes" : "No"),
      ];
      if (r.promptText) {
        items.push(new EventDetailItem("プロンプト (先頭)", r.promptText.slice(0, 80)));
      }
      return items;
    }
    return [];
  }
}

// ---------------------------------------------------------------------------
// Status Tree
// ---------------------------------------------------------------------------

class StatusItem extends vscode.TreeItem {
  constructor(label: string, detail: string, icon: string) {
    super(label, vscode.TreeItemCollapsibleState.None);
    this.description = detail;
    this.iconPath = new vscode.ThemeIcon(icon);
  }
}

class StatusProvider implements vscode.TreeDataProvider<vscode.TreeItem> {
  private _onChange = new vscode.EventEmitter<void>();
  readonly onDidChangeTreeData = this._onChange.event;
  private alive = false;
  private stats: Stats | null = null;
  private meta: AdminMeta | null = null;

  update(alive: boolean, stats: Stats | null, meta: AdminMeta | null) {
    this.alive = alive;
    this.stats = stats;
    this.meta = meta;
    this._onChange.fire();
  }

  getTreeItem(e: vscode.TreeItem): vscode.TreeItem { return e; }

  getChildren(): vscode.TreeItem[] {
    const s = this.stats;
    const m = this.meta;
    return [
      new StatusItem("プロキシ", this.alive ? "🟢 起動中" : "🔴 停止", this.alive ? "server-process" : "server-environment"),
      new StatusItem("Guard URL", `http://${cfg("proxyAddr", "127.0.0.1:8787")}`, "link"),
      ...(m ? [
        new StatusItem("モデル", m.model, "symbol-class"),
        new StatusItem("バックエンド", m.backend, "chip"),
      ] : []),
      ...(s ? [
        new StatusItem("合計リクエスト", `${s.total}`, "list-ordered"),
        new StatusItem("ブロック", `${s.blocked} (${(s.blockRate * 100).toFixed(1)}%)`, "error"),
        new StatusItem("通過", `${s.allowed}`, "pass"),
        new StatusItem("平均レイテンシ", `${s.avgLatencyMs.toFixed(0)} ms`, "pulse"),
        new StatusItem("P95レイテンシ", `${s.p95LatencyMs} ms`, "dashboard"),
      ] : []),
    ];
  }
}

// ---------------------------------------------------------------------------
// Proxy process management
// ---------------------------------------------------------------------------

let proxyProcess: cp.ChildProcess | undefined;
let outputChannel: vscode.OutputChannel;

function proxyDir(): string {
  const ws = vscode.workspace.workspaceFolders?.[0]?.uri.fsPath ?? "";
  return path.join(ws, "proxy-server");
}

function startProxy() {
  if (proxyProcess) return;
  const classifier = cfg<string>("classifier", "");
  const args: string[] = ["-File", "start.ps1"];
  if (classifier) args.push("-Classifier", classifier);

  outputChannel.appendLine("[PromptGate] start.ps1 を起動します...");
  proxyProcess = cp.spawn("powershell.exe", args, {
    cwd: proxyDir(),
    stdio: ["ignore", "pipe", "pipe"],
    windowsHide: true,
  });
  proxyProcess.stdout?.on("data", (d: Buffer) => outputChannel.append(d.toString()));
  proxyProcess.stderr?.on("data", (d: Buffer) => outputChannel.append(d.toString()));
  proxyProcess.on("exit", (code) => {
    outputChannel.appendLine(`[PromptGate] プロセス終了 (code ${code})`);
    proxyProcess = undefined;
  });
}

function stopProxy() {
  if (!proxyProcess) return;
  outputChannel.appendLine("[PromptGate] プロキシを停止します...");
  // kill the process tree (llama-server + proxy.exe + web)
  cp.exec(`taskkill /PID ${proxyProcess.pid} /T /F`, () => { /* best-effort */ });
  proxyProcess = undefined;
}

// ---------------------------------------------------------------------------
// Status bar
// ---------------------------------------------------------------------------

let statusBar: vscode.StatusBarItem;

function updateStatusBar(alive: boolean, stats: Stats | null) {
  const block = stats?.blocked ?? 0;
  const total = stats?.total ?? 0;
  statusBar.text = alive
    ? `$(shield) Guard ON${total > 0 ? ` · ${block} ブロック / ${total}` : ""}`
    : `$(shield-x) Guard OFF`;
  statusBar.backgroundColor = alive && block > 0
    ? new vscode.ThemeColor("statusBarItem.warningBackground")
    : undefined;
  statusBar.show();
}

// ---------------------------------------------------------------------------
// Activate
// ---------------------------------------------------------------------------

export function activate(context: vscode.ExtensionContext) {
  outputChannel = vscode.window.createOutputChannel("PromptGate");
  context.subscriptions.push(outputChannel);

  statusBar = vscode.window.createStatusBarItem(vscode.StatusBarAlignment.Left, 100);
  statusBar.command = "promptgate.openAdminUI";
  statusBar.tooltip = "PromptGate Guard — クリックで管理UIを開く";
  context.subscriptions.push(statusBar);

  const logProvider = new ActivityLogProvider();
  const statusProvider = new StatusProvider();
  context.subscriptions.push(
    vscode.window.registerTreeDataProvider("promptgate.activityLog", logProvider),
    vscode.window.registerTreeDataProvider("promptgate.status", statusProvider),
  );

  // Commands
  context.subscriptions.push(
    vscode.commands.registerCommand("promptgate.startProxy", () => {
      startProxy();
      vscode.window.showInformationMessage("PromptGate: start.ps1 を起動しました。出力はパネルを確認してください。");
      outputChannel.show(true);
    }),
    vscode.commands.registerCommand("promptgate.stopProxy", () => {
      stopProxy();
      vscode.window.showInformationMessage("PromptGate: プロキシを停止しました。");
    }),
    vscode.commands.registerCommand("promptgate.openTerminal", () => {
      // Open a terminal with ANTHROPIC_BASE_URL pre-set so Claude Code routes through proxy
      const addr = cfg("proxyAddr", "127.0.0.1:8787");
      const term = vscode.window.createTerminal({
        name: "Claude (PromptGate)",
        env: { ANTHROPIC_BASE_URL: `http://${addr}` },
        cwd: vscode.workspace.workspaceFolders?.[0]?.uri.fsPath,
      });
      term.show();
      term.sendText("# ANTHROPIC_BASE_URL is set — Claude Code routes through PromptGate");
    }),
    vscode.commands.registerCommand("promptgate.openAdminUI", () => {
      const url = cfg<string>("adminUiUrl", "http://127.0.0.1:3939");
      vscode.env.openExternal(vscode.Uri.parse(url));
    }),
    vscode.commands.registerCommand("promptgate.refreshLog", async () => {
      await poll(logProvider, statusProvider);
    }),
  );

  // Auto-start
  if (cfg("autoStartProxy", false)) {
    startProxy();
  }

  // Polling loop
  const interval = cfg<number>("pollIntervalMs", 3000);
  const timer = setInterval(() => poll(logProvider, statusProvider), interval);
  context.subscriptions.push({ dispose: () => clearInterval(timer) });
  context.subscriptions.push({ dispose: () => stopProxy() });

  // Initial render (offline state)
  updateStatusBar(false, null);
  statusProvider.update(false, null, null);
}

let lastBlockEventId = "";

async function poll(log: ActivityLogProvider, status: StatusProvider) {
  const base = adminBase();

  let alive = false;
  let stats: Stats | null = null;
  let meta: AdminMeta | null = null;
  let page: EventPage | null = null;

  try {
    [meta, stats, page] = await Promise.all([
      httpGet<AdminMeta>(`${base}/admin/meta`),
      httpGet<Stats>(`${base}/admin/stats`),
      httpGet<EventPage>(`${base}/admin/events?limit=30&decision=`),
    ]);
    alive = true;
  } catch {
    // proxy not running
  }

  if (page) log.update(page);
  status.update(alive, stats, meta);
  updateStatusBar(alive, stats);

  // Notify on new block events
  if (page && page.events.length > 0) {
    const newest = page.events[0];
    if (newest.decision === "BLOCK" && newest.eventId !== lastBlockEventId) {
      lastBlockEventId = newest.eventId;
      void vscode.window.showWarningMessage(
        `[PromptGate] ブロック: ${newest.reason?.slice(0, 60) ?? newest.source}`,
        "ログを見る",
        "管理UI"
      ).then((sel) => {
        if (sel === "ログを見る") outputChannel.show();
        if (sel === "管理UI") vscode.commands.executeCommand("promptgate.openAdminUI");
      });
    }
  }
}

export function deactivate() {
  stopProxy();
}
