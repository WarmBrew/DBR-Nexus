import { Routes, Route, Navigate } from 'react-router-dom';
import { useAuthStore } from './store/authSlice';
import AppLayout from './components/Layout/AppLayout';
import Login from './pages/Login';
import Dashboard from './pages/Dashboard';
import Devices from './pages/Devices';
import DeviceDetail from './pages/DeviceDetail';
import Tunnels from './pages/Tunnels';
import Users from './pages/Users';
import AuditLogs from './pages/AuditLogs';
import Settings from './pages/Settings';

function ProtectedRoute({ children }: { children: React.ReactNode }) {
  const token = useAuthStore((s) => s.token);
  if (!token) return <Navigate to="/login" replace />;
  return <>{children}</>;
}

export default function App() {
  return (
    <Routes>
      <Route path="/login" element={<Login />} />
      <Route
        path="/"
        element={
          <ProtectedRoute>
            <AppLayout />
          </ProtectedRoute>
        }
      >
        <Route index element={<Dashboard />} />
        <Route path="devices" element={<Devices />} />
        <Route path="devices/:id" element={<DeviceDetail />} />
        <Route path="tunnels" element={<Tunnels />} />
        <Route path="users" element={<Users />} />
        <Route path="audit" element={<AuditLogs />} />
        <Route path="settings" element={<Settings />} />
      </Route>
    </Routes>
  );
}
