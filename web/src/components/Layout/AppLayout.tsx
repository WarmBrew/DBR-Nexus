import { useState, useEffect } from 'react';
import { Outlet, useNavigate, useLocation } from 'react-router-dom';
import { Layout, Menu, Avatar, Dropdown, theme, Tag } from 'antd';
import {
  DashboardOutlined,
  DesktopOutlined,
  ApiOutlined,
  UserOutlined,
  AuditOutlined,
  SettingOutlined,
  LogoutOutlined,
  WifiOutlined,
} from '@ant-design/icons';
import { useAuthStore } from '../../store/authSlice';
import wsManager from '../../api/ws';

const { Header, Sider, Content } = Layout;

export default function AppLayout() {
  const [collapsed, setCollapsed] = useState(false);
  const [latency, setLatency] = useState(-1);
  const navigate = useNavigate();
  const location = useLocation();
  const { user, logout } = useAuthStore();
  const { token: themeToken } = theme.useToken();

  // Auto-connect WebSocket if token exists (handles page refresh)
  const token = useAuthStore((s) => s.token);
  useEffect(() => {
    if (token && !wsManager.isConnected()) {
      wsManager.connect(token);
    }
    wsManager.setOnLatencyChange(setLatency);
    return () => wsManager.setOnLatencyChange(() => {});
  }, [token]);

  const menuItems = [
    { key: '/', icon: <DashboardOutlined />, label: '仪表盘' },
    { key: '/devices', icon: <DesktopOutlined />, label: '设备' },
    { key: '/tunnels', icon: <ApiOutlined />, label: '端口转发' },
    ...(user?.role === 'admin'
      ? [
          { key: '/users', icon: <UserOutlined />, label: '用户管理' },
          { key: '/audit', icon: <AuditOutlined />, label: '审计日志' },
        ]
      : []),
    { key: '/settings', icon: <SettingOutlined />, label: '系统设置' },
  ];

  const selectedKey = menuItems.find((item) =>
    location.pathname.startsWith(item.key) && item.key !== '/'
      ? true
      : location.pathname === item.key,
  )?.key || '/';

  const handleLogout = () => {
    logout();
    navigate('/login');
  };

  const userMenuItems = [
    { key: 'profile', icon: <UserOutlined />, label: user?.display_name || user?.username },
    { type: 'divider' as const },
    { key: 'logout', icon: <LogoutOutlined />, label: '退出登录', onClick: handleLogout },
  ];

  const isWorkbenchPage = location.pathname.startsWith('/devices/') && location.pathname !== '/devices';

  return (
    <Layout style={{ minHeight: '100vh' }}>
      <Sider
        collapsible
        collapsed={collapsed}
        onCollapse={setCollapsed}
        theme="dark"
        style={{ background: '#1e1e1e', borderRight: `1px solid ${themeToken.colorBorder}` }}
      >
        <div style={{ height: 64, display: 'flex', alignItems: 'center', justifyContent: 'center', borderBottom: `1px solid ${themeToken.colorBorder}` }}>
          <DesktopOutlined style={{ fontSize: 24, color: themeToken.colorPrimary }} />
          {!collapsed && <span style={{ marginLeft: 8, fontSize: 16, fontWeight: 600, color: '#cccccc' }}>DBR Nexus</span>}
        </div>
        <Menu
          mode="inline"
          theme="dark"
          selectedKeys={[selectedKey]}
          items={menuItems}
          onClick={({ key }) => navigate(key)}
          style={{ border: 'none', background: '#1e1e1e' }}
        />
      </Sider>
      <Layout>
        <Header style={{ padding: '0 24px', background: '#252526', display: 'flex', justifyContent: 'flex-end', alignItems: 'center', gap: 16, borderBottom: `1px solid ${themeToken.colorBorder}` }}>
          {latency >= 0 && (
            <span style={{ fontSize: 12, color: latency < 50 ? '#52c41a' : latency < 150 ? '#faad14' : '#ff4d4f', display: 'flex', alignItems: 'center', gap: 4 }}>
              <WifiOutlined />
              {latency}ms
            </span>
          )}
          <Dropdown menu={{ items: userMenuItems }} placement="bottomRight">
            <Avatar icon={<UserOutlined />} style={{ cursor: 'pointer', background: '#3c3c3c' }} />
          </Dropdown>
        </Header>
        <Content style={isWorkbenchPage
          ? { padding: 0, background: '#1e1e1e', overflow: 'hidden', flex: 1 }
          : { margin: 16, padding: 24, background: '#252526', borderRadius: 8, overflow: 'auto', flex: 1 }
        }>
          <Outlet />
        </Content>
      </Layout>
    </Layout>
  );
}
