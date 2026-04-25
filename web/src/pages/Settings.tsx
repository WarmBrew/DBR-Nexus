import { useEffect, useState } from 'react';
import { Card, Form, Input, Select, Button, Descriptions, Typography, message, Divider, Space, Tag, Switch, Alert } from 'antd';
import { SettingOutlined, SafetyOutlined, CloudServerOutlined, ApiOutlined, UserOutlined, LockOutlined } from '@ant-design/icons';
import { useAuthStore } from '../store/authSlice';
import apiClient from '../api/client';

const { Title, Text } = Typography;

interface SystemSettings {
  server_addr: string;
  tunnel_bind_range: string;
  jwt_expiry: string;
  heartbeat_timeout: string;
  tls_enabled: boolean;
  psk_configured: boolean;
  log_level: string;
  web_access_enabled: boolean;
  web_access_allowed_ips: string;
}

export default function Settings() {
  const user = useAuthStore((s) => s.user);
  const [settings, setSettings] = useState<SystemSettings | null>(null);
  const [form] = Form.useForm();
  const [loading, setLoading] = useState(false);
  const isAdmin = user?.role === 'admin';

  const fetchSettings = async () => {
    try {
      const { data } = await apiClient.get<SystemSettings>('/settings');
      setSettings(data);
      form.setFieldsValue({
        tunnel_bind_range: data.tunnel_bind_range,
        heartbeat_timeout: data.heartbeat_timeout,
        log_level: data.log_level,
        web_access_enabled: data.web_access_enabled,
        web_access_allowed_ips: data.web_access_allowed_ips,
      });
    } catch { /* ignore */ }
  };

  useEffect(() => { fetchSettings(); }, []);

  const handleSave = async (values: any) => {
    setLoading(true);
    try {
      await apiClient.put('/settings', values);
      message.success('设置已保存');
      fetchSettings();
    } catch (e: any) {
      message.error(e.response?.data?.error || '保存失败');
    } finally {
      setLoading(false);
    }
  };

  return (
    <div>
      <Title level={4}><SettingOutlined style={{ marginRight: 8 }} />系统设置</Title>

      {/* User Info */}
      <Card title={<><UserOutlined style={{ marginRight: 8 }} />当前用户</>} style={{ marginBottom: 16 }}>
        <Descriptions column={2}>
          <Descriptions.Item label="用户名">{user?.username}</Descriptions.Item>
          <Descriptions.Item label="显示名">{user?.display_name}</Descriptions.Item>
          <Descriptions.Item label="角色">
            <Tag color={user?.role === 'admin' ? 'red' : user?.role === 'operator' ? 'blue' : 'green'}>
              {user?.role === 'admin' ? '管理员' : user?.role === 'operator' ? '操作员' : '查看者'}
            </Tag>
          </Descriptions.Item>
        </Descriptions>
      </Card>

      {/* Server Info */}
      <Card title={<><CloudServerOutlined style={{ marginRight: 8 }} />服务器信息</>} style={{ marginBottom: 16 }}>
        <Descriptions column={2}>
          <Descriptions.Item label="监听地址"><Text code>{settings?.server_addr || '-'}</Text></Descriptions.Item>
          <Descriptions.Item label="TLS"><Tag color={settings?.tls_enabled ? 'green' : 'orange'}>{settings?.tls_enabled ? '已启用' : '未启用'}</Tag></Descriptions.Item>
          <Descriptions.Item label="JWT 有效期"><Text code>{settings?.jwt_expiry || '-'}</Text></Descriptions.Item>
          <Descriptions.Item label="PSK"><Tag color={settings?.psk_configured ? 'green' : 'red'}>{settings?.psk_configured ? '已配置' : '未配置'}</Tag></Descriptions.Item>
        </Descriptions>
      </Card>

      {/* Editable Settings */}
      {isAdmin && (
        <Card title={<><SafetyOutlined style={{ marginRight: 8 }} />系统配置</>} style={{ marginBottom: 16 }}>
          <Form form={form} onFinish={handleSave} layout="vertical">
            <Form.Item name="tunnel_bind_range" label={<><ApiOutlined style={{ marginRight: 4 }} />隧道端口范围</>} rules={[{ required: true, message: '请输入端口范围' }]}>
              <Input placeholder="127.0.0.1:10000-20000" />
            </Form.Item>
            <Form.Item name="heartbeat_timeout" label="心跳超时" rules={[{ required: true, message: '请输入超时时间' }]}>
              <Input placeholder="90s" />
            </Form.Item>
            <Form.Item name="log_level" label="日志级别" rules={[{ required: true, message: '请选择日志级别' }]}>
              <Select options={[
                { value: 'debug', label: 'Debug' },
                { value: 'info', label: 'Info' },
                { value: 'warn', label: 'Warn' },
                { value: 'error', label: 'Error' },
              ]} />
            </Form.Item>
            <Form.Item>
              <Button type="primary" htmlType="submit" loading={loading}>保存设置</Button>
            </Form.Item>
          </Form>
        </Card>
      )}

      {/* Web Access Control */}
      {isAdmin && (
        <Card title={<><LockOutlined style={{ marginRight: 8 }} />访问控制</>}>
          <Alert
            type="info"
            showIcon
            message="访问控制仅限制 Web 管理界面（API 和前端页面），不影响设备连接和心跳。本机（127.0.0.1 / ::1）始终允许访问。"
            style={{ marginBottom: 16 }}
          />
          <Form form={form} onFinish={handleSave} layout="vertical">
            <Form.Item name="web_access_enabled" label="启用 IP 白名单" valuePropName="checked">
              <Switch checkedChildren="开启" unCheckedChildren="关闭" />
            </Form.Item>
            <Form.Item
              noStyle
              shouldUpdate={(prev, cur) => prev.web_access_enabled !== cur.web_access_enabled}
            >
              {({ getFieldValue }) =>
                getFieldValue('web_access_enabled') ? (
                  <Form.Item
                    name="web_access_allowed_ips"
                    label="允许的 IP / CIDR 列表"
                    rules={[
                      { required: true, message: '启用白名单后必须配置允许的 IP' },
                      {
                        validator: (_, value) => {
                          if (!value) return Promise.resolve();
                          const entries = value.split(',').map((s: string) => s.trim()).filter(Boolean);
                          const ipRegex = /^(\d{1,3}\.){3}\d{1,3}$/;
                          const ipv6Regex = /^([0-9a-fA-F]{0,4}:){2,7}[0-9a-fA-F]{0,4}$/;
                          const cidrRegex = /^(\d{1,3}\.){3}\d{1,3}\/\d{1,3}$/;
                          const cidr6Regex = /^([0-9a-fA-F]{0,4}:){2,7}[0-9a-fA-F]{0,4}\/\d{1,3}$/;
                          for (const entry of entries) {
                            if (!ipRegex.test(entry) && !ipv6Regex.test(entry) && !cidrRegex.test(entry) && !cidr6Regex.test(entry)) {
                              return Promise.reject(new Error(`格式错误: "${entry}"，请使用 IP 或 CIDR 格式（如 192.168.1.0/24）`));
                            }
                          }
                          return Promise.resolve();
                        },
                      },
                    ]}
                    extra="逗号分隔，支持 IP（如 192.168.1.100）和 CIDR（如 192.168.1.0/24）。"
                  >
                    <Input placeholder="192.168.1.0/24, 10.0.0.1" />
                  </Form.Item>
                ) : null
              }
            </Form.Item>
            <Form.Item>
              <Button type="primary" htmlType="submit" loading={loading}>保存设置</Button>
            </Form.Item>
          </Form>
        </Card>
      )}
    </div>
  );
}
