export type RuntimeConfig = {
  ippListen: string;
  adminListen: string;
};

export type Driver = {
  id: string;
  name: string;
  transport: string;
};

export type PrinterStatus = {
  connected: boolean;
  connecting: boolean;
  lastError?: string;
  busy: boolean;
  info?: { name: string; id: string; address: string; mtu: number | null };
};

export type Printer = {
  id: number;
  name: string;
  slug: string;
  uuid: string;
  driver: string;
  deviceId: number | null;
  profileId: number;
  enabled: boolean;
  advertise: boolean;
  airPrint: boolean;
  status: PrinterStatus;
  queueDepth: number;
  updatedAt: number;
};

export type Device = {
  id: number;
  nativeId: string;
  name: string;
  address: string;
  writeUuid: string;
  notifyUuid: string | null;
  assignedPrinterId: number | null;
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
};

export type Job = {
  id: number;
  printerId: number;
  name: string;
  userName: string;
  state: string;
  documentFormat: string;
  copies: number;
  bytes: number;
  error: string | null;
  createdAt: number;
  startedAt: number | null;
  completedAt: number | null;
};

export type Status = {
  version: string;
  config: RuntimeConfig;
  drivers: Driver[];
  printers: Printer[];
  devices: Device[];
  profiles: Profile[];
  jobs: Job[];
};

export type PrinterInput = Pick<Printer, "name" | "slug" | "driver" | "deviceId" | "profileId" | "enabled" | "advertise" | "airPrint">;
export type ProfileInput = Omit<Profile, "id">;
