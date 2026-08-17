export type Settings = {
  ippName: string;
  printerUuid: string;
  ippListen: string;
  adminListen: string;
  advertise: boolean;
  airPrint: boolean;
  updatedAt: number;
};

export type PrinterStatus = {
  connected: boolean;
  connecting: boolean;
  lastError?: string;
  busy: boolean;
  info?: { name: string; id: string; address: string; mtu: number };
};

export type Device = {
  id: number;
  nativeId: string;
  name: string;
  address: string;
  writeUuid: string;
  notifyUuid: string | null;
  selected: boolean;
  lastSeenAt: number;
};

export type ScannedDevice = {
  id: string;
  name: string;
  address: string;
  rssi: number;
};

export type Profile = {
  id: number;
  name: string;
  widthMm: number;
  heightMm: number;
  gapMm: number;
  paperType: number;
  darkness: number;
  speed: number;
  active: boolean;
};

export type Job = {
  id: number;
  name: string;
  userName: string;
  state: string;
  documentFormat: string;
  copies: number;
  bytes: number;
  error: string | null;
  createdAt: number;
};

export type Status = {
  version: string;
  settings: Settings;
  printer: PrinterStatus;
  devices: Device[];
  profiles: Profile[];
  jobs: Job[];
  queueDepth: number;
};

export type ProfileInput = Omit<Profile, "id" | "active">;
