import { create } from 'zustand';
import apiClient from '../api/client';
import type { LoginResponse, User } from '../api/types';
import wsManager from '../api/ws';

interface AuthState {
  token: string | null;
  user: User | null;
  login: (username: string, password: string) => Promise<void>;
  logout: () => void;
}

export const useAuthStore = create<AuthState>((set) => ({
  token: localStorage.getItem('token'),
  user: JSON.parse(localStorage.getItem('user') || 'null'),

  login: async (username: string, password: string) => {
    const { data } = await apiClient.post<LoginResponse>('/auth/login', { username, password });
    set({ token: data.token, user: data.user });
    localStorage.setItem('token', data.token);
    localStorage.setItem('user', JSON.stringify(data.user));
    wsManager.connect(data.token);
  },

  logout: () => {
    set({ token: null, user: null });
    localStorage.removeItem('token');
    localStorage.removeItem('user');
    wsManager.disconnect();
  },
}));
