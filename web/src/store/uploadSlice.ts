import { create } from 'zustand';

export interface UploadTask {
  id: string;
  name: string;
  deviceId: string;
  percent: number;
  status: 'uploading' | 'completed' | 'error';
  error?: string;
  startTime: number;
}

interface UploadState {
  tasks: UploadTask[];
  addTask: (task: UploadTask) => void;
  updateTask: (id: string, updates: Partial<UploadTask>) => void;
  removeTask: (id: string) => void;
  clearCompleted: () => void;
}

export const useUploadStore = create<UploadState>((set) => ({
  tasks: [],
  addTask: (task) => set((state) => ({ tasks: [...state.tasks, task] })),
  updateTask: (id, updates) => set((state) => ({
    tasks: state.tasks.map((t) => t.id === id ? { ...t, ...updates } : t),
  })),
  removeTask: (id) => set((state) => ({
    tasks: state.tasks.filter((t) => t.id !== id),
  })),
  clearCompleted: () => set((state) => ({
    tasks: state.tasks.filter((t) => t.status === 'uploading'),
  })),
}));

export default useUploadStore;