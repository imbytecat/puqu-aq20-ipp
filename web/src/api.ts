import type { ProfileInput, ScannedDevice, Settings, Status } from "./types";

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, {
    ...init,
    headers: init?.body ? { "Content-Type": "application/json", ...init.headers } : init?.headers,
  });
  const data = (await response.json()) as T & { error?: string };
  if (!response.ok) throw new Error(data.error || `HTTP ${response.status}`);
  return data;
}

export const api = {
  status: () => request<Status>("/api/status"),
  scan: () => request<{ devices: ScannedDevice[] }>("/api/bluetooth/scan", { method: "POST" }),
  saveDevice: (device: ScannedDevice) =>
    request("/api/devices", {
      method: "PUT",
      body: JSON.stringify({ nativeId: device.id, name: device.name, address: device.address, selected: true }),
    }),
  selectDevice: (id: number) => request(`/api/devices/${id}/select`, { method: "POST" }),
  deleteDevice: (id: number) => request(`/api/devices/${id}`, { method: "DELETE" }),
  connect: () => request("/api/printer/connect", { method: "POST" }),
  testPrint: () => request("/api/printer/test", { method: "POST" }),
  createProfile: (profile: ProfileInput) =>
    request("/api/profiles", { method: "POST", body: JSON.stringify(profile) }),
  updateProfile: (id: number, profile: ProfileInput) =>
    request(`/api/profiles/${id}`, { method: "PUT", body: JSON.stringify(profile) }),
  activateProfile: (id: number) => request(`/api/profiles/${id}/activate`, { method: "POST" }),
  deleteProfile: (id: number) => request(`/api/profiles/${id}`, { method: "DELETE" }),
  updateSettings: (settings: Settings) =>
    request<{ restartRequired: boolean }>("/api/settings", {
      method: "PUT",
      body: JSON.stringify({
        ippName: settings.ippName,
        ippListen: settings.ippListen,
        adminListen: settings.adminListen,
        advertise: settings.advertise,
        airPrint: settings.airPrint,
      }),
    }),
  cancelJob: (id: number) => request(`/api/jobs/${id}/cancel`, { method: "POST" }),
};
