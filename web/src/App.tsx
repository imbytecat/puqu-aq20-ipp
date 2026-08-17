import { useCallback, useEffect, useState } from "react";
import { api } from "./api";
import type { Profile, ProfileInput, ScannedDevice, Settings, Status } from "./types";

const blankProfile: ProfileInput = {
  name: "",
  widthMm: 40,
  heightMm: 30,
  gapMm: 2,
  paperType: 2,
  darkness: 8,
  speed: 3,
};

export function App() {
  const [status, setStatus] = useState<Status | null>(null);
  const [settings, setSettings] = useState<Settings | null>(null);
  const [scanResults, setScanResults] = useState<ScannedDevice[]>([]);
  const [profile, setProfile] = useState<ProfileInput>(blankProfile);
  const [editingProfile, setEditingProfile] = useState<number | null>(null);
  const [working, setWorking] = useState("");
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");

  const refresh = useCallback(async () => {
    try {
      const next = await api.status();
      setStatus(next);
      setSettings((current) => current ?? next.settings);
      setError("");
    } catch (cause) {
      setError(message(cause));
    }
  }, []);

  useEffect(() => {
    void refresh();
    const timer = window.setInterval(() => void refresh(), 2500);
    return () => window.clearInterval(timer);
  }, [refresh]);

  async function run(name: string, action: () => Promise<unknown>, success = "Saved") {
    setWorking(name);
    setError("");
    setNotice("");
    try {
      await action();
      await refresh();
      setNotice(success);
    } catch (cause) {
      setError(message(cause));
    } finally {
      setWorking("");
    }
  }

  async function scan() {
    setWorking("scan");
    setError("");
    try {
      const result = await api.scan();
      setScanResults(result.devices);
      setNotice(`Found ${result.devices.length} Bluetooth device${result.devices.length === 1 ? "" : "s"}`);
    } catch (cause) {
      setError(message(cause));
    } finally {
      setWorking("");
    }
  }

  function editProfile(value?: Profile) {
    if (!value) {
      setEditingProfile(null);
      setProfile(blankProfile);
      return;
    }
    setEditingProfile(value.id);
    setProfile({
      name: value.name,
      widthMm: value.widthMm,
      heightMm: value.heightMm,
      gapMm: value.gapMm,
      paperType: value.paperType,
      darkness: value.darkness,
      speed: value.speed,
    });
  }

  if (!status || !settings) {
    return <main className="loading">{error || "Loading bridge status…"}</main>;
  }

  const printerState = status.printer.connected
    ? status.printer.busy
      ? "Printing"
      : "Connected"
    : status.printer.connecting
      ? "Connecting"
      : "Offline";
  const activeProfile = status.profiles.find((profile) => profile.active);
  const activeLabel = activeProfile ? `${activeProfile.widthMm} × ${activeProfile.heightMm} mm` : "Not configured";


  return (
    <div className="shell">
      <header>
        <div>
          <p className="eyebrow">PUQU AQ20</p>
          <h1>IPP Bridge</h1>
          <p className="subtitle">Bluetooth label printer, available to every system print dialog.</p>
        </div>
        <div className={`state state-${printerState.toLowerCase()}`}>
          <span />
          <div>
            <strong>{printerState}</strong>
            <small>{status.printer.info?.name || status.printer.lastError || "No printer selected"}</small>
          </div>
        </div>
      </header>

      {(error || notice) && (
        <div className={error ? "message error" : "message success"} role="status">
          {error || notice}
          <button onClick={() => { setError(""); setNotice(""); }} aria-label="Dismiss message">×</button>
        </div>
      )}

      <section className="metrics" aria-label="Bridge summary">
        <Metric label="IPP printer" value={status.settings.ippName} detail={`ipp://${location.hostname}${status.settings.ippListen}/ipp/print`} />
        <Metric label="Queue" value={`${status.queueDepth} waiting`} detail={`${status.jobs.filter((job) => job.state === "completed").length} recent completed`} />
        <Metric label="Label" value={activeLabel} detail="Active media profile" />
        <Metric label="Version" value={status.version} detail={status.settings.advertise ? "Network discovery on" : "Network discovery off"} />
      </section>

      <div className="grid">
        <Section title="Bluetooth printer" description="Select one device. The daemon reconnects automatically.">
          <div className="actions">
            <Button busy={working === "scan"} onClick={scan}>Scan for devices</Button>
            <Button kind="secondary" busy={working === "connect"} onClick={() => run("connect", api.connect, "Reconnect requested")}>Reconnect</Button>
            <Button kind="secondary" busy={working === "test"} disabled={!status.printer.connected} onClick={() => run("test", api.testPrint, "Test label printed")}>Print test label</Button>
          </div>

          {scanResults.length > 0 && (
            <div className="device-list scan-list">
              {scanResults.map((device) => (
                <div className="device" key={`${device.id}-${device.address}`}>
                  <div><strong>{device.name || "Unnamed device"}</strong><small>{device.address} · {device.rssi} dBm</small></div>
                  <Button kind="secondary" onClick={() => run(`save-${device.id}`, () => api.saveDevice(device), "Printer saved")}>Use printer</Button>
                </div>
              ))}
            </div>
          )}

          <div className="device-list">
            {status.devices.length === 0 && <p className="empty">No saved printer. Start a scan with the printer powered on.</p>}
            {status.devices.map((device) => (
              <div className="device" key={device.id}>
                <div>
                  <strong>{device.name || device.nativeId} {device.selected && <em>Selected</em>}</strong>
                  <small>{device.address} · write {device.writeUuid} · notify {device.notifyUuid || "auto"}</small>
                </div>
                <div className="row-actions">
                  {!device.selected && <button className="link" onClick={() => run(`select-${device.id}`, () => api.selectDevice(device.id))}>Select</button>}
                  <button className="link danger" onClick={() => run(`delete-device-${device.id}`, () => api.deleteDevice(device.id), "Printer removed")}>Remove</button>
                </div>
              </div>
            ))}
          </div>
        </Section>

        <Section title="Network printer" description="IPP and Bonjour settings. Listener changes apply after restart.">
          <form onSubmit={(event) => {
            event.preventDefault();
            void run("settings", () => api.updateSettings(settings), "Settings saved; restart bridge to apply listener changes");
          }}>
            <Field label="Printer name"><input value={settings.ippName} onChange={(event) => setSettings({ ...settings, ippName: event.target.value })} required /></Field>
            <div className="field-pair">
              <Field label="IPP listener"><input value={settings.ippListen} onChange={(event) => setSettings({ ...settings, ippListen: event.target.value })} required /></Field>
              <Field label="Admin listener"><input value={settings.adminListen} onChange={(event) => setSettings({ ...settings, adminListen: event.target.value })} required /></Field>
            </div>
            <label className="toggle"><input type="checkbox" checked={settings.advertise} onChange={(event) => setSettings({ ...settings, advertise: event.target.checked })} /><span />Advertise with DNS-SD</label>
            <label className="toggle"><input type="checkbox" checked={settings.airPrint} onChange={(event) => setSettings({ ...settings, airPrint: event.target.checked })} /><span />Advertise AirPrint compatibility</label>
            <p className="hint">Admin UI remains local-only by default: <code>127.0.0.1:8080</code>.</p>
            <Button busy={working === "settings"} type="submit">Save network settings</Button>
          </form>
        </Section>

        <Section title="Label profiles" description="The active profile defines fixed media dimensions exposed over IPP.">
          <div className="profile-list">
            {status.profiles.map((item) => (
              <div className={`profile-card ${item.active ? "active" : ""}`} key={item.id}>
                <div><strong>{item.name}</strong><small>{item.widthMm} × {item.heightMm} mm · {item.gapMm} mm gap</small></div>
                <div className="row-actions">
                  {!item.active && <button className="link" onClick={() => run(`activate-${item.id}`, () => api.activateProfile(item.id), "Profile activated")}>Activate</button>}
                  <button className="link" onClick={() => editProfile(item)}>Edit</button>
                  {!item.active && <button className="link danger" onClick={() => run(`delete-profile-${item.id}`, () => api.deleteProfile(item.id), "Profile removed")}>Remove</button>}
                </div>
              </div>
            ))}
          </div>
          <form className="profile-form" onSubmit={(event) => {
            event.preventDefault();
            const save = editingProfile ? () => api.updateProfile(editingProfile, profile) : () => api.createProfile(profile);
            void run("profile", save, editingProfile ? "Profile updated" : "Profile created").then(() => editProfile());
          }}>
            <h3>{editingProfile ? "Edit profile" : "New profile"}</h3>
            <Field label="Name"><input value={profile.name} onChange={(event) => setProfile({ ...profile, name: event.target.value })} required /></Field>
            <div className="field-triple">
              <NumberField label="Width (mm)" value={profile.widthMm} min={2} max={255} step={0.1} set={(widthMm) => setProfile({ ...profile, widthMm })} />
              <NumberField label="Height (mm)" value={profile.heightMm} min={2} max={255} step={0.1} set={(heightMm) => setProfile({ ...profile, heightMm })} />
              <NumberField label="Gap (mm)" value={profile.gapMm} min={0} max={20} step={0.1} set={(gapMm) => setProfile({ ...profile, gapMm })} />
            </div>
            <div className="field-triple">
              <NumberField label="Darkness" value={profile.darkness} min={0} max={15} set={(darkness) => setProfile({ ...profile, darkness })} />
              <NumberField label="Speed" value={profile.speed} min={0} max={15} set={(speed) => setProfile({ ...profile, speed })} />
              <Field label="Paper type"><select value={profile.paperType} onChange={(event) => setProfile({ ...profile, paperType: Number(event.target.value) })}><option value={1}>Continuous</option><option value={2}>Gap</option><option value={3}>Mark</option></select></Field>
            </div>
            <div className="actions"><Button busy={working === "profile"} type="submit">{editingProfile ? "Update profile" : "Create profile"}</Button>{editingProfile && <Button kind="secondary" onClick={() => editProfile()}>Cancel</Button>}</div>
          </form>
        </Section>

        <Section title="Recent print jobs" description="Jobs arrive from the operating system through IPP.">
          <div className="jobs">
            {status.jobs.length === 0 && <p className="empty">No print jobs yet.</p>}
            {status.jobs.map((job) => (
              <div className="job" key={job.id}>
                <span className={`job-state job-${job.state}`}>{job.state}</span>
                <div><strong>#{job.id} {job.name}</strong><small>{job.userName || "local user"} · {job.copies} cop{job.copies === 1 ? "y" : "ies"} · {formatBytes(job.bytes)}</small>{job.error && <small className="job-error">{job.error}</small>}</div>
                {(job.state === "pending" || job.state === "processing") && <button className="link danger" onClick={() => run(`cancel-${job.id}`, () => api.cancelJob(job.id), "Job canceled")}>Cancel</button>}
              </div>
            ))}
          </div>
        </Section>
      </div>

      <footer>Printer UUID {status.settings.printerUuid}</footer>
    </div>
  );
}

function Section({ title, description, children }: { title: string; description: string; children: React.ReactNode }) {
  return <section className="panel"><div className="panel-title"><h2>{title}</h2><p>{description}</p></div>{children}</section>;
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return <label className="field"><span>{label}</span>{children}</label>;
}

function NumberField({ label, value, set, min, max, step = 1 }: { label: string; value: number; set: (value: number) => void; min: number; max: number; step?: number }) {
  return <Field label={label}><input type="number" value={value} min={min} max={max} step={step} onChange={(event) => set(Number(event.target.value))} required /></Field>;
}

function Button({ busy, kind = "primary", children, ...props }: React.ButtonHTMLAttributes<HTMLButtonElement> & { busy?: boolean; kind?: "primary" | "secondary" }) {
  return <button className={`button ${kind}`} disabled={busy || props.disabled} {...props}>{busy ? "Working…" : children}</button>;
}

function Metric({ label, value, detail }: { label: string; value: string; detail: string }) {
  return <div className="metric"><span>{label}</span><strong>{value}</strong><small>{detail}</small></div>;
}


function formatBytes(bytes: number) {
  if (bytes < 1024) return `${bytes} B`;
  return `${(bytes / 1024).toFixed(1)} KiB`;
}

function message(cause: unknown) {
  return cause instanceof Error ? cause.message : String(cause);
}
