import { create } from 'zustand';
import apiClient from '../api/client';
import type { Device } from '../api/types';

interface DeviceState {
  devices: Device[];
  loading: boolean;
  fetchDevices: () => Promise<void>;
  updateDeviceStatus: (id: string, status: string) => void;
}

export const useDeviceStore = create<DeviceState>((set, get) => ({
  devices: [],
  loading: false,

  fetchDevices: async () => {
    set({ loading: true });
    try {
      const { data } = await apiClient.get<Device[]>('/devices');
      set({ devices: data || [], loading: false });
    } catch {
      set({ loading: false });
    }
  },

  updateDeviceStatus: (id: string, status: string) => {
    set((state) => ({
      devices: state.devices.map((d) =>
        d.id === id ? { ...d, status: status as Device['status'] } : d,
      ),
    }));
  },
}));
