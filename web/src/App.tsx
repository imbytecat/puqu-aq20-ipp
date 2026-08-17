import { useEffect, useState } from "react";
import { queryOptions, useMutation, useQuery, useQueryClient, type UseMutationResult } from "@tanstack/react-query";
import { Link, Outlet, useNavigate, useParams } from "@tanstack/react-router";
import copyToClipboard from "copy-to-clipboard";
import { Check, Copy, Laptop, MonitorCog, Terminal, type LucideIcon } from "lucide-react";
import { api } from "./api";
import type { Printer, PrinterInput, Profile, ProfileInput, Status } from "./types";

type CopyMutation = UseMutationResult<void, Error, string>;

const statusQuery = queryOptions({ queryKey: ["status"], queryFn: api.status, refetchInterval: 2500 });
const blankProfile: ProfileInput = {
  name: "",
  widthMm: 40,
  heightMm: 30,
  gapMm: 2,
  paperType: 2,
  darkness: 8,
  speed: 3,
};

function useStatus() {
  return useQuery(statusQuery);
}

function useRefreshMutation<TData, TVariables = void>(mutationFn: (variables: TVariables) => Promise<TData>) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: statusQuery.queryKey }),
  });
}

export function RootLayout() {
  const status = useStatus();
  const activeJobs = status.data?.jobs.filter((job) => job.state === "pending" || job.state === "processing").length ?? 0;
  return (
    <div className="app-shell">
      <aside className="sidebar">
        <div className="brand"><span className="brand-mark">P</span><div><strong>PUQU</strong><small>打印服务</small></div></div>
        <nav aria-label="主导航">
          <NavLink to="/" exact>概览</NavLink>
          <NavLink to="/printers" count={status.data?.printers.length}>打印机</NavLink>
          <NavLink to="/devices" count={status.data?.devices.length}>设备</NavLink>
          <NavLink to="/profiles" count={status.data?.profiles.length}>标签规格</NavLink>
          <NavLink to="/jobs" count={activeJobs}>打印任务</NavLink>
          <NavLink to="/runtime">运行配置</NavLink>
        </nav>
        <div className="sidebar-foot">版本 {status.data?.version ?? "—"}</div>
      </aside>
      <main className="content">
        <header className="topbar">
          <div><p className="eyebrow">DRIVERLESS IPP · BLUETOOTH LE</p><h1>标签打印管理</h1></div>
          <div className={`service-pill ${status.isError ? "offline" : "online"}`}><span />{status.isError ? "服务异常" : "服务运行中"}</div>
        </header>
        {status.error && <Feedback error={status.error} />}
        <Outlet />
      </main>
    </div>
  );
}

function NavLink({ to, exact, count, children }: { to: string; exact?: boolean; count?: number; children: React.ReactNode }) {
  return <Link to={to} activeOptions={{ exact }} activeProps={{ className: "active" }}>{children}{count !== undefined && <span>{count}</span>}</Link>;
}

export function DashboardPage() {
  const status = useStatus();
  if (!status.data) return <Loading />;
  const connected = status.data.printers.filter((item) => item.status.connected).length;
  const activeJobs = status.data.jobs.filter((job) => job.state === "pending" || job.state === "processing").length;
  const completed = status.data.jobs.filter((job) => job.state === "completed").length;
  return (
    <Page title="系统概览" description="一个服务实例可管理多台物理打印机，并为每台打印机发布独立 IPP 队列。">
      <div className="metrics">
        <Metric label="已配置打印机" value={String(status.data.printers.length)} detail={`${connected} 台已连接`} />
        <Metric label="进行中任务" value={String(activeJobs)} detail={`${completed} 个近期完成`} />
        <Metric label="已保存设备" value={String(status.data.devices.length)} detail="BLE 物理端点" />
        <Metric label="标签规格" value={String(status.data.profiles.length)} detail="可被多台打印机复用" />
      </div>
      <Panel title="打印机状态" description="每台打印机拥有独立连接、队列和 IPP 地址。">
        <div className="card-grid">
          {status.data.printers.map((item) => <PrinterCard key={item.id} printer={item} status={status.data} />)}
          {status.data.printers.length === 0 && <Empty>尚未创建打印机。</Empty>}
        </div>
        <div className="panel-actions"><Link className="button primary" to="/printers">管理打印机</Link></div>
      </Panel>
      <Panel title="最近任务" description="来自系统打印对话框的 IPP 作业。">
        <JobList status={status.data} limit={6} />
      </Panel>
    </Page>
  );
}

export function PrintersPage() {
  const status = useStatus();
  const navigate = useNavigate();
  const create = useRefreshMutation((value: PrinterInput) => api.createPrinter(value));
  if (!status.data) return <Loading />;
  const initial: PrinterInput = {
    name: "",
    slug: "",
    driver: status.data.drivers[0]?.id ?? "puqu-aq20",
    deviceId: null,
    profileId: status.data.profiles[0]?.id ?? 0,
    enabled: true,
  };
  async function save(value: PrinterInput) {
    const created = await create.mutateAsync(value);
    await navigate({ to: "/printers/$printerId", params: { printerId: String(created.id) } });
  }
  return (
    <Page title="打印机" description="打印机是对操作系统可见的逻辑队列；每台打印机绑定一个驱动、设备和标签规格。">
      <div className="card-grid">
        {status.data.printers.map((item) => <PrinterCard key={item.id} printer={item} status={status.data} />)}
      </div>
      <Panel title="添加打印机" description="当前支持 PUQU AQ20 / PQ / TQ / Q 系列 BLE 驱动。">
        <PrinterForm initial={initial} status={status.data} saving={create.isPending} onSave={save} />
        <Feedback error={create.error} />
      </Panel>
    </Page>
  );
}

export function PrinterPage() {
  const status = useStatus();
  const navigate = useNavigate();
  const { printerId } = useParams({ strict: false }) as { printerId?: string };
  const id = Number(printerId);
  const configured = status.data?.printers.find((item) => item.id === id);
  const [draft, setDraft] = useState<PrinterInput | null>(null);
  useEffect(() => {
    if (configured) setDraft(toPrinterInput(configured));
  }, [configured?.id, configured?.updatedAt]);
  const update = useRefreshMutation((value: PrinterInput) => api.updatePrinter(id, value));
  const remove = useRefreshMutation(() => api.deletePrinter(id));
  const connect = useRefreshMutation(() => api.connect(id));
  const test = useRefreshMutation(() => api.testPrint(id));
  const copy = useMutation({ mutationFn: copyText });
  if (!status.data) return <Loading />;
  if (!configured || !draft) return <Empty>打印机不存在。</Empty>;
  const printerName = configured.name;
  const profile = status.data.profiles.find((item) => item.id === configured.profileId);
  const device = status.data.devices.find((item) => item.id === configured.deviceId);
  const uri = printerURI(status.data, configured);
  const commands = installCommands(configured, uri);
  async function removePrinter() {
    if (!window.confirm(`删除打印机“${printerName}”及其任务记录？`)) return;
    await remove.mutateAsync();
    await navigate({ to: "/printers" });
  }
  return (
    <Page title={configured.name} description={uri} back="/printers">
      <div className="metrics compact">
        <Metric label="连接状态" value={printerState(configured)} detail={configured.status.info?.name || configured.status.lastError || "尚未绑定设备"} />
        <Metric label="物理设备" value={device?.name || "未分配"} detail={device?.address || "可先在设备页添加"} />
        <Metric label="标签规格" value={profile ? `${profile.widthMm} × ${profile.heightMm} mm` : "不可用"} detail={profile?.name || "请选择规格"} />
        <Metric label="队列" value={`${configured.queueDepth} 等待`} detail={`UUID ${configured.uuid.slice(0, 8)}`} />
      </div>
      <Panel title="安装到系统" description="复制对应系统的安装命令；自动发现未启用。">
        <div className="install-commands">
          <InstallCommand icon={Terminal} label="Linux" detail="CUPS · IPP Everywhere" value={commands.linux} copy={copy} />
          <InstallCommand icon={Laptop} label="macOS" detail="CUPS · Driverless" value={commands.macos} copy={copy} />
          <InstallCommand icon={MonitorCog} label="Windows" detail="Windows 10/11 · Internet Printing Client" value={commands.windows} copy={copy} />
        </div>
        <div className="actions">
          <Button kind="secondary" busy={connect.isPending} onClick={() => connect.mutate()}>重新连接</Button>
          <Button kind="secondary" busy={test.isPending} disabled={!configured.status.connected} onClick={() => test.mutate()}>打印测试标签</Button>
        </div>
        <Feedback error={copy.error || connect.error || test.error} success={connect.isSuccess ? "已请求重新连接。" : test.isSuccess ? "测试标签已发送。" : ""} />
      </Panel>
      <Panel title="打印机设置" description="队列名称创建后保持不变，避免操作系统保存的 IPP 地址失效。">
        <PrinterForm initial={draft} status={status.data} currentId={id} fixedSlug saving={update.isPending} onChange={setDraft} onSave={(value) => update.mutateAsync(value)} />
        <Feedback error={update.error} success={update.isSuccess ? "打印机设置已保存。" : ""} />
      </Panel>
      <Panel tone="danger" title="删除打印机" description="会删除此逻辑队列和历史任务，不会删除物理设备或标签规格。">
        <Button kind="danger" busy={remove.isPending} onClick={removePrinter}>删除打印机</Button>
        <Feedback error={remove.error} />
      </Panel>
    </Page>
  );
}

export function DevicesPage() {
  const status = useStatus();
  const scan = useMutation({ mutationFn: api.scan });
  const save = useRefreshMutation(api.saveDevice);
  const remove = useRefreshMutation((id: number) => api.deleteDevice(id));
  if (!status.data) return <Loading />;
  return (
    <Page title="设备" description="保存发现到的 BLE 设备，再在打印机设置中完成绑定。一个物理设备只能分配给一台打印机。">
      <Panel title="扫描蓝牙" description="请先开启打印机并放在此电脑附近。扫描期间其他连接会短暂等待。">
        <Button busy={scan.isPending} onClick={() => scan.mutate()}>扫描附近设备</Button>
        <Feedback error={scan.error} success={scan.isSuccess ? `发现 ${scan.data.devices.length} 个设备。` : ""} />
        {scan.data && <div className="list inset">
          {scan.data.devices.map((device) => (
            <div className="list-row" key={`${device.id}-${device.address}`}>
              <div><strong>{device.name || "未命名设备"}</strong><small>{device.address} · {device.rssi} dBm</small></div>
              <Button kind="secondary" busy={save.isPending && save.variables?.id === device.id} onClick={() => save.mutate(device)}>保存设备</Button>
            </div>
          ))}
          {scan.data.devices.length === 0 && <Empty>未发现设备。</Empty>}
        </div>}
      </Panel>
      <Panel title="已保存设备" description="删除已分配设备会自动解除对应打印机的绑定。">
        <div className="list">
          {status.data.devices.map((device) => {
            const owner = status.data.printers.find((item) => item.id === device.assignedPrinterId);
            return <div className="list-row" key={device.id}>
              <div><strong>{device.name || device.nativeId}</strong><small>{device.address} · 写入 {device.writeUuid} · 通知 {device.notifyUuid || "自动"}</small>{owner && <span className="tag">分配给 {owner.name}</span>}</div>
              <Button kind="ghost-danger" onClick={() => window.confirm(`删除设备“${device.name}”？`) && remove.mutate(device.id)}>删除</Button>
            </div>;
          })}
          {status.data.devices.length === 0 && <Empty>还没有保存设备。</Empty>}
        </div>
        <Feedback error={save.error || remove.error} success={save.isSuccess ? "设备已保存。" : remove.isSuccess ? "设备已删除。" : ""} />
      </Panel>
    </Page>
  );
}

export function ProfilesPage() {
  const status = useStatus();
  const [editing, setEditing] = useState<number | null>(null);
  const [draft, setDraft] = useState<ProfileInput>(blankProfile);
  const create = useRefreshMutation((value: ProfileInput) => api.createProfile(value));
  const update = useRefreshMutation((value: ProfileInput) => api.updateProfile(editing!, value));
  const remove = useRefreshMutation((id: number) => api.deleteProfile(id));
  if (!status.data) return <Loading />;
  function edit(profile?: Profile) {
    setEditing(profile?.id ?? null);
    setDraft(profile ? { name: profile.name, widthMm: profile.widthMm, heightMm: profile.heightMm, gapMm: profile.gapMm, paperType: profile.paperType, darkness: profile.darkness, speed: profile.speed } : blankProfile);
  }
  async function saveProfile() {
    if (editing) await update.mutateAsync(draft); else await create.mutateAsync(draft);
    edit();
  }
  return (
    <Page title="标签规格" description="规格可被多台打印机复用；每台打印机独立选择当前装载的标签。">
      <Panel title="规格列表" description="正在被打印机使用的规格不能删除。">
        <div className="list">
          {status.data.profiles.map((profile) => {
            const users = status.data.printers.filter((item) => item.profileId === profile.id);
            return <div className="list-row" key={profile.id}>
              <div><strong>{profile.name}</strong><small>{profile.widthMm} × {profile.heightMm} mm · 间隙 {profile.gapMm} mm · 浓度 {profile.darkness} · 速度 {profile.speed}</small>{users.length > 0 && <span className="tag">{users.length} 台打印机使用</span>}</div>
              <div className="row-actions"><Button kind="secondary" onClick={() => edit(profile)}>编辑</Button><Button kind="ghost-danger" disabled={users.length > 0} onClick={() => window.confirm(`删除规格“${profile.name}”？`) && remove.mutate(profile.id)}>删除</Button></div>
            </div>;
          })}
        </div>
      </Panel>
      <Panel title={editing ? "编辑规格" : "新建规格"} description="1 mm = 8 个打印点；尺寸会作为固定介质能力发布给 IPP 客户端。">
        <ProfileForm value={draft} set={setDraft} saving={create.isPending || update.isPending} onSave={saveProfile} onCancel={editing ? () => edit() : undefined} />
        <Feedback error={create.error || update.error || remove.error} success={create.isSuccess ? "规格已创建。" : update.isSuccess ? "规格已更新。" : remove.isSuccess ? "规格已删除。" : ""} />
      </Panel>
    </Page>
  );
}

export function JobsPage() {
  const status = useStatus();
  const cancel = useRefreshMutation((id: number) => api.cancelJob(id));
  if (!status.data) return <Loading />;
  return (
    <Page title="打印任务" description="所有打印机的近期 IPP 作业；任务始终归属于提交时选择的打印机。">
      <Panel title="任务记录" description="每台打印机内部按顺序处理，不同打印机可并行工作。">
        <JobList status={status.data} onCancel={(id) => cancel.mutate(id)} />
        <Feedback error={cancel.error} success={cancel.isSuccess ? "任务已取消。" : ""} />
      </Panel>
    </Page>
  );
}

export function RuntimePage() {
  const status = useStatus();
  if (!status.data) return <Loading />;
  return (
    <Page title="运行配置" description="启动配置由 Koanf 统一加载；修改后重启服务，不写入 SQLite，也不在管理界面热修改。">
      <div className="metrics compact">
        <Metric label="配置文件" value={status.data.config.configFileLoaded ? "已加载" : "使用默认值"} detail={status.data.config.configFile} />
        <Metric label="IPP 监听" value={status.data.config.ippListen} detail="系统打印客户端连接入口" />
        <Metric label="管理界面监听" value={status.data.config.adminListen} detail="管理后台连接入口" />
        <Metric label="数据库" value={status.data.config.dataPath} detail="仅保存业务状态和任务历史" />
        <Metric label="日志级别" value={status.data.config.logLevel} detail="debug / info / warn / error" />
        <Metric label="配置优先级" value="CLI > 环境变量 > TOML" detail="最后回退到安全默认值" />
      </div>
      <Panel title="配置入口" description="系统服务只持久化配置文件路径，避免安装参数覆盖后续文件修改。">
        <pre><code>{`puqu-ipp --config ${JSON.stringify(status.data.config.configFile)}`}</code></pre>
        <p className="hint">环境变量：<code>PUQU_CONFIG</code>、<code>PUQU_DATA_PATH</code>、<code>PUQU_IPP_LISTEN</code>、<code>PUQU_ADMIN_LISTEN</code>、<code>PUQU_LOG_LEVEL</code>。</p>
      </Panel>
      <Panel title="数据归属" description="进程配置和业务状态保持单一数据源。">
        <div className="split-list"><div><strong>Koanf 启动配置</strong><p>配置文件路径、监听地址、数据库路径、日志级别。</p></div><div><strong>SQLite 业务状态</strong><p>打印机、设备、标签规格和任务记录。</p></div></div>
      </Panel>
    </Page>
  );
}

function PrinterForm({ initial, status, currentId, fixedSlug, saving, onChange, onSave }: { initial: PrinterInput; status: Status; currentId?: number; fixedSlug?: boolean; saving: boolean; onChange?: (value: PrinterInput) => void; onSave: (value: PrinterInput) => Promise<unknown> }) {
  const [value, setValue] = useState(initial);
  useEffect(() => setValue(initial), [initial.name, initial.slug, initial.deviceId, initial.profileId, initial.enabled]);
  function change(next: PrinterInput) { setValue(next); onChange?.(next); }
  return <form onSubmit={(event) => { event.preventDefault(); void onSave(value); }}>
    <div className="field-pair"><Field label="显示名称"><input value={value.name} onChange={(event) => change({ ...value, name: event.target.value })} required /></Field><Field label="队列名称"><input value={value.slug} disabled={fixedSlug} onChange={(event) => change({ ...value, slug: event.target.value })} placeholder="shipping-labels" required /></Field></div>
    <div className="field-triple"><Field label="驱动"><select value={value.driver} onChange={(event) => change({ ...value, driver: event.target.value })}>{status.drivers.map((driver) => <option key={driver.id} value={driver.id}>{driver.name}</option>)}</select></Field><Field label="物理设备"><select value={value.deviceId ?? ""} onChange={(event) => change({ ...value, deviceId: event.target.value ? Number(event.target.value) : null })}><option value="">暂不分配</option>{status.devices.map((device) => <option key={device.id} value={device.id} disabled={device.assignedPrinterId !== null && device.assignedPrinterId !== currentId}>{device.name || device.nativeId}{device.assignedPrinterId !== null && device.assignedPrinterId !== currentId ? "（已占用）" : ""}</option>)}</select></Field><Field label="标签规格"><select value={value.profileId || ""} onChange={(event) => change({ ...value, profileId: Number(event.target.value) })} required><option value="" disabled>请选择</option>{status.profiles.map((profile) => <option key={profile.id} value={profile.id}>{profile.name} · {profile.widthMm}×{profile.heightMm} mm</option>)}</select></Field></div>
    <div className="toggles"><Toggle checked={value.enabled} set={(enabled) => change({ ...value, enabled })}>启用队列</Toggle></div>
    <Button busy={saving} type="submit">保存打印机</Button>
  </form>;
}

function ProfileForm({ value, set, saving, onSave, onCancel }: { value: ProfileInput; set: (value: ProfileInput) => void; saving: boolean; onSave: () => Promise<void>; onCancel?: () => void }) {
  return <form onSubmit={(event) => { event.preventDefault(); void onSave(); }}>
    <Field label="名称"><input value={value.name} onChange={(event) => set({ ...value, name: event.target.value })} required /></Field>
    <div className="field-triple"><NumberField label="宽度（mm）" value={value.widthMm} min={2} max={255} step={0.1} set={(widthMm) => set({ ...value, widthMm })} /><NumberField label="高度（mm）" value={value.heightMm} min={2} max={255} step={0.1} set={(heightMm) => set({ ...value, heightMm })} /><NumberField label="间隙（mm）" value={value.gapMm} min={0} max={20} step={0.1} set={(gapMm) => set({ ...value, gapMm })} /></div>
    <div className="field-triple"><NumberField label="浓度" value={value.darkness} min={0} max={11} set={(darkness) => set({ ...value, darkness })} /><NumberField label="速度" value={value.speed} min={0} max={5} set={(speed) => set({ ...value, speed })} /><Field label="纸张类型"><select value={value.paperType} onChange={(event) => set({ ...value, paperType: Number(event.target.value) })}><option value={1}>连续纸</option><option value={2}>间隙纸</option><option value={3}>黑标纸</option></select></Field></div>
    <div className="actions"><Button busy={saving} type="submit">{onCancel ? "更新规格" : "创建规格"}</Button>{onCancel && <Button kind="secondary" onClick={onCancel}>取消</Button>}</div>
  </form>;
}

function PrinterCard({ printer, status }: { printer: Printer; status: Status }) {
  const profile = status.profiles.find((item) => item.id === printer.profileId);
  return <Link className="printer-card" to="/printers/$printerId" params={{ printerId: String(printer.id) }}>
    <div className="card-head"><StatusDot printer={printer} /><span className="tag">{printer.driver}</span></div>
    <h3>{printer.name}</h3><p>{printer.status.info?.name || printer.status.lastError || "未绑定设备"}</p>
    <dl><div><dt>IPP</dt><dd>/ipp/{printer.slug}</dd></div><div><dt>规格</dt><dd>{profile ? `${profile.widthMm}×${profile.heightMm} mm` : "不可用"}</dd></div><div><dt>队列</dt><dd>{printer.queueDepth}</dd></div></dl>
  </Link>;
}

function JobList({ status, limit, onCancel }: { status: Status; limit?: number; onCancel?: (id: number) => void }) {
  const jobs = limit ? status.jobs.slice(0, limit) : status.jobs;
  return <div className="list jobs">{jobs.map((job) => {
    const owner = status.printers.find((item) => item.id === job.printerId);
    return <div className="list-row" key={job.id}><span className={`job-state job-${job.state}`}>{jobState(job.state)}</span><div><strong>#{job.id} {job.name}</strong><small>{owner?.name || "已删除打印机"} · {job.userName || "本地用户"} · {job.copies} 份 · {formatBytes(job.bytes)}</small>{job.error && <small className="job-error">{job.error}</small>}</div>{onCancel && (job.state === "pending" || job.state === "processing") && <Button kind="ghost-danger" onClick={() => onCancel(job.id)}>取消</Button>}</div>;
  })}{jobs.length === 0 && <Empty>还没有打印任务。</Empty>}</div>;
}

function Page({ title, description, back, children }: { title: string; description: string; back?: string; children: React.ReactNode }) {
  return <div className="page">{back && <Link className="back" to={back}>← 返回</Link>}<div className="page-title"><h2>{title}</h2><p>{description}</p></div>{children}</div>;
}
function Panel({ title, description, tone, children }: { title: string; description: string; tone?: "danger"; children: React.ReactNode }) {
  return <section className={`panel ${tone || ""}`}><div className="panel-title"><h2>{title}</h2><p>{description}</p></div>{children}</section>;
}
function Metric({ label, value, detail }: { label: string; value: string; detail: string }) { return <div className="metric"><span>{label}</span><strong>{value}</strong><small>{detail}</small></div>; }
function Field({ label, children }: { label: string; children: React.ReactNode }) { return <label className="field"><span>{label}</span>{children}</label>; }
function NumberField({ label, value, set, min, max, step = 1 }: { label: string; value: number; set: (value: number) => void; min: number; max: number; step?: number }) { return <Field label={label}><input type="number" value={value} min={min} max={max} step={step} onChange={(event) => set(Number(event.target.value))} required /></Field>; }
function Toggle({ checked, set, children }: { checked: boolean; set: (value: boolean) => void; children: React.ReactNode }) { return <label className="toggle"><input type="checkbox" checked={checked} onChange={(event) => set(event.target.checked)} /><span />{children}</label>; }
function Button({ busy, kind = "primary", children, ...props }: React.ButtonHTMLAttributes<HTMLButtonElement> & { busy?: boolean; kind?: "primary" | "secondary" | "danger" | "ghost-danger" }) { return <button type={props.type || "button"} className={`button ${kind}`} disabled={busy || props.disabled} {...props}>{busy ? "处理中…" : children}</button>; }
function InstallCommand({ icon: Icon, label, detail, value, copy }: { icon: LucideIcon; label: string; detail: string; value: string; copy: CopyMutation }) {
  const copied = copy.isSuccess && copy.variables === value;
  return <div className="install-command">
    <div className="install-command-head">
      <div className="install-command-title"><Icon size={18} aria-hidden="true" /><div><strong>{label}</strong><small>{detail}</small></div></div>
      <Button kind="secondary" busy={copy.isPending && copy.variables === value} onClick={() => copy.mutate(value)}>
        {copied ? <Check size={14} aria-hidden="true" /> : <Copy size={14} aria-hidden="true" />}{copied ? "已复制" : "复制"}
      </Button>
    </div>
    <code>{value}</code>
  </div>;
}
function Feedback({ error, success }: { error?: unknown; success?: string }) { if (!error && !success) return null; return <div className={`message ${error ? "error" : "success"}`} role="status">{error ? message(error) : success}</div>; }
function Empty({ children }: { children: React.ReactNode }) { return <p className="empty">{children}</p>; }
function Loading() { return <div className="loading">正在读取打印服务状态…</div>; }
function StatusDot({ printer }: { printer: Printer }) { return <span className={`status-dot ${printer.status.connected ? printer.status.busy ? "busy" : "connected" : printer.status.connecting ? "busy" : "offline"}`}>{printerState(printer)}</span>; }

function toPrinterInput(value: Printer): PrinterInput { return { name: value.name, slug: value.slug, driver: value.driver, deviceId: value.deviceId, profileId: value.profileId, enabled: value.enabled }; }
function printerState(value: Printer) { if (!value.enabled) return "已停用"; if (value.status.connected) return value.status.busy ? "打印中" : "已连接"; return value.status.connecting ? "连接中" : "离线"; }
function printerURI(status: Status, printer: Printer) { const port = status.config.ippListen.split(":").at(-1); return `ipp://${window.location.hostname}:${port}/ipp/${printer.slug}`; }
function installCommands(printer: Printer, uri: string) {
  const cups = `sudo lpadmin -p ${shellQuote(printer.slug)} -E -v ${shellQuote(uri)} -m everywhere`;
  return {
    linux: cups,
    macos: cups,
    windows: `rundll32.exe printui.dll,PrintUIEntry /in /n \"${uri.replace(/^ipp:/, "http:")}\"`,
  };
}
function shellQuote(value: string) { return `'${value.replaceAll("'", `'\"'\"'`)}'`; }
async function copyText(value: string) {
  const copied = await copyToClipboard(value, {
    fallbackToPrompt: true,
    message: "按 #{key} 复制安装命令，然后按 Enter",
  });
  if (!copied) throw new Error("无法复制安装命令");
}
function jobState(value: string) { return ({ pending: "等待", processing: "打印中", completed: "完成", canceled: "已取消", aborted: "失败" } as Record<string, string>)[value] || value; }
function formatBytes(bytes: number) { return bytes < 1024 ? `${bytes} B` : `${(bytes / 1024).toFixed(1)} KiB`; }
function message(cause: unknown) { return cause instanceof Error ? cause.message : String(cause); }
