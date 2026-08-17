import type { Printer, PrinterInput, Profile, ProfileInput, ScannedDevice, Status } from "./types";

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
      method: "POST",
      body: JSON.stringify({ nativeId: device.id, name: device.name || device.id, address: device.address }),
    }),
  deleteDevice: (id: number) => request(`/api/devices/${id}`, { method: "DELETE" }),
  createPrinter: (printer: PrinterInput) =>
    request<Printer>("/api/printers", { method: "POST", body: JSON.stringify(printer) }),
  updatePrinter: (id: number, printer: PrinterInput) =>
    request<Printer>(`/api/printers/${id}`, { method: "PUT", body: JSON.stringify(printer) }),
  deletePrinter: (id: number) => request(`/api/printers/${id}`, { method: "DELETE" }),
  connect: (id: number) => request(`/api/printers/${id}/connect`, { method: "POST" }),
  testPrint: (id: number) => request(`/api/printers/${id}/test`, { method: "POST" }),
  createProfile: (profile: ProfileInput) =>
    request<Profile>("/api/profiles", { method: "POST", body: JSON.stringify(profile) }),
  updateProfile: (id: number, profile: ProfileInput) =>
    request<Profile>(`/api/profiles/${id}`, { method: "PUT", body: JSON.stringify(profile) }),
  deleteProfile: (id: number) => request(`/api/profiles/${id}`, { method: "DELETE" }),
  cancelJob: (id: number) => request(`/api/jobs/${id}/cancel`, { method: "POST" }),
};
