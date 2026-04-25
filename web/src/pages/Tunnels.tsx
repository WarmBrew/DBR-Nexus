import { useEffect, useState, useCallback } from 'react';
import { Table, Button, Modal, Form, Input, Popconfirm, Typography, message, Card, Tag, Select, Space, Badge, Row, Col, Dropdown, Alert } from 'antd';
import { PlusOutlined, ArrowRightOutlined, LinkOutlined, DisconnectOutlined, ReloadOutlined, SwapOutlined, GlobalOutlined, LockOutlined, CopyOutlined, ClockCircleOutlined, RetweetOutlined, DeleteOutlined, PlayCircleOutlined, FieldTimeOutlined } from '@ant-design/icons';
import apiClient from '../api/client';
import type { Tunnel } from '../api/types';

const { Title, Text } = Typography;

interface DeviceOption {
  id: string;
  hostname: string;
  status: string;
}

interface Socks5CreateResult {
  id: string;
  local_addr: string;
  type: string;
  socks5_user: string;
  socks5_pass: string;
  auth: boolean;
  allowed_ips: string;
  expires_at: string;
}

// CountdownTimer component - displays time remaining until expiration
function CountdownTimer({ expiresAt, onExpired }: { expiresAt: string | null; onExpired?: () => void }) {
  const [remaining, setRemaining] = useState<number | null>(null);

  useEffect(() => {
    if (!expiresAt) {
      setRemaining(null);
      return;
    }

    const calc = () => {
      const diff = new Date(expiresAt).getTime() - Date.now();
      if (diff <= 0) {
        setRemaining(0);
        onExpired?.();
        return false;
      }
      setRemaining(diff);
      return true;
    };

    if (!calc()) return;
    const timer = setInterval(() => {
      if (!calc()) clearInterval(timer);
    }, 1000);

    return () => clearInterval(timer);
  }, [expiresAt, onExpired]);

  if (remaining === null) return <Text type="secondary">-</Text>;
  if (remaining <= 0) return <Text type="danger" style={{ fontSize: 12 }}>已过期</Text>;

  const hours = Math.floor(remaining / 3600000);
  const mins = Math.floor((remaining % 3600000) / 60000);
  const secs = Math.floor((remaining % 60000) / 1000);
  const formatted = `${hours.toString().padStart(2, '0')}:${mins.toString().padStart(2, '0')}:${secs.toString().padStart(2, '0')}`;

  const totalMinutes = remaining / 60000;
  let color = '#52c41a'; // green
  let style: React.CSSProperties = { fontSize: 12, fontFamily: 'monospace' };
  if (totalMinutes < 5) {
    color = '#ff4d4f'; // red
    style.animation = 'blink 1s infinite';
  } else if (totalMinutes < 10) {
    color = '#faad14'; // orange
  }

  return <span style={{ ...style, color }}>{formatted}</span>;
}

export default function Tunnels() {
  const [tunnels, setTunnels] = useState<Tunnel[]>([]);
  const [devices, setDevices] = useState<DeviceOption[]>([]);
  const [form] = Form.useForm();
  const [socks5Form] = Form.useForm();
  const [createVisible, setCreateVisible] = useState(false);
  const [socks5Visible, setSocks5Visible] = useState(false);
  const [socks5Result, setSocks5Result] = useState<Socks5CreateResult | null>(null);

  const fetchTunnels = useCallback(async () => {
    try {
      const { data } = await apiClient.get<Tunnel[]>('/tunnels');
      setTunnels(data || []);
    } catch { /* ignore */ }
  }, []);

  const fetchDevices = useCallback(async () => {
    try {
      const { data } = await apiClient.get('/devices');
      setDevices((data || []).map((d: any) => ({ id: d.id, hostname: d.hostname || d.id.slice(0, 8), status: d.status })));
    } catch { /* ignore */ }
  }, []);

  useEffect(() => {
    fetchTunnels();
    fetchDevices();
    const interval = setInterval(fetchTunnels, 10000);
    return () => clearInterval(interval);
  }, [fetchTunnels, fetchDevices]);

  const handleCreate = async (values: { device_id: string; remote_addr: string }) => {
    try {
      await apiClient.post('/tunnels', values);
      message.success('隧道创建成功，30分钟后将自动关闭');
      setCreateVisible(false);
      form.resetFields();
      fetchTunnels();
    } catch (e: any) {
      message.error(e.response?.data?.error || '创建失败');
    }
  };

  const handleCreateSocks5 = async (values: { device_id: string; socks5_user: string; socks5_pass: string; allowed_ips: string }) => {
    try {
      const { data } = await apiClient.post<Socks5CreateResult>('/tunnels/socks5', values);
      setSocks5Visible(false);
      socks5Form.resetFields();
      setSocks5Result(data);
      fetchTunnels();
    } catch (e: any) {
      message.error(e.response?.data?.error || 'SOCKS5代理创建失败');
    }
  };

  const copyToClipboard = (text: string) => {
    navigator.clipboard.writeText(text).then(() => message.success('已复制'));
  };

  const handleDelete = async (id: string) => {
    try {
      await apiClient.delete(`/tunnels/${id}`);
      message.success('隧道已关闭');
      fetchTunnels();
    } catch {
      message.error('关闭隧道失败');
    }
  };

  const handleRenew = async (id: string, hours: number) => {
    try {
      await apiClient.put(`/tunnels/${id}/renew`, { duration_hours: hours });
      message.success(`已续期 ${hours} 小时`);
      fetchTunnels();
    } catch (e: any) {
      message.error(e.response?.data?.error || '续期失败');
    }
  };

  const handleHardDelete = async (id: string) => {
    try {
      await apiClient.delete(`/tunnels/${id}/permanent`);
      message.success('记录已永久删除');
      fetchTunnels();
    } catch (e: any) {
      message.error(e.response?.data?.error || '删除失败');
    }
  };

  const handleRestart = (tunnel: Tunnel) => {
    if (tunnel.tunnel_type === 'socks5') {
      socks5Form.setFieldsValue({ device_id: tunnel.device_id });
      setSocks5Visible(true);
    } else {
      form.setFieldsValue({ device_id: tunnel.device_id, remote_addr: tunnel.remote_addr });
      setCreateVisible(true);
    }
  };

  const getDeviceName = (deviceId: string) => {
    const d = devices.find(d => d.id === deviceId);
    return d?.hostname || deviceId.slice(0, 8);
  };

  const activeTunnels = tunnels.filter(t => t.state === 'active');

  // Renew menu items for SOCKS5 tunnels
  const renewMenuItems = (tunnelId: string) => [
    { key: '1', label: '+1小时', onClick: () => handleRenew(tunnelId, 1) },
    { key: '2', label: '+2小时', onClick: () => handleRenew(tunnelId, 2) },
    { key: '3', label: '+3小时', onClick: () => handleRenew(tunnelId, 3) },
    { key: '4', label: '+4小时', onClick: () => handleRenew(tunnelId, 4) },
    { key: '5', label: '+5小时', onClick: () => handleRenew(tunnelId, 5) },
  ];

  const columns = [
    { title: 'ID', dataIndex: 'id', key: 'id', render: (v: string) => <Text code style={{ fontSize: 11 }}>{v.slice(0, 8)}</Text> },
    { title: '设备', dataIndex: 'device_id', key: 'device_id', render: (v: string) => (
      <Space>
        <Badge status={devices.find(d => d.id === v)?.status === 'online' ? 'success' : 'error'} />
        <Text>{getDeviceName(v)}</Text>
      </Space>
    )},
    { title: '本地地址', dataIndex: 'local_addr', key: 'local_addr', render: (v: string) => <Text code style={{ color: '#61afef' }}>{v}</Text> },
    { title: '远程地址', dataIndex: 'remote_addr', key: 'remote_addr', render: (v: string, record: Tunnel) => (
      <Space size={4}>
        {record.tunnel_type === 'socks5' ?
          <Tag color="purple" icon={<GlobalOutlined />}>SOCKS5 代理</Tag> :
          <Text code style={{ color: '#e5c07b' }}>{v}</Text>
        }
        {record.socks5_auth && <Tag color="orange" icon={<LockOutlined />} style={{ fontSize: 10 }}>认证</Tag>}
      </Space>
    )},
    { title: '认证用户', dataIndex: 'socks5_user', key: 'socks5_user', render: (v: string) => v ? <Text code style={{ fontSize: 11 }}>{v}</Text> : <Text type="secondary">-</Text> },
    { title: '状态', dataIndex: 'state', key: 'state', render: (v: string) => (
      <Tag color={v === 'active' ? 'green' : 'default'} icon={v === 'active' ? <LinkOutlined /> : <DisconnectOutlined />}>
        {v === 'active' ? '活跃' : '已关闭'}
      </Tag>
    )},
    { title: '剩余时间', key: 'countdown', render: (_: any, record: Tunnel) => (
      record.state === 'active' ? <CountdownTimer expiresAt={record.expires_at} onExpired={fetchTunnels} /> : <Text type="secondary">-</Text>
    )},
    { title: '发送', dataIndex: 'bytes_sent', key: 'bytes_sent', render: (v: number) => v ? `${(v / 1024).toFixed(1)}KB` : '0' },
    { title: '接收', dataIndex: 'bytes_recv', key: 'bytes_recv', render: (v: number) => v ? `${(v / 1024).toFixed(1)}KB` : '0' },
    {
      title: '操作', key: 'action',
      render: (_: any, record: Tunnel) => record.state === 'active' ? (
        <Space size={4}>
          <Popconfirm title="确定关闭隧道?" onConfirm={() => handleDelete(record.id)}>
            <Button size="small" danger icon={<DisconnectOutlined />}>关闭</Button>
          </Popconfirm>
          {record.tunnel_type === 'socks5' && (
            <Dropdown menu={{ items: renewMenuItems(record.id) }} trigger={['click']}>
              <Button size="small" icon={<FieldTimeOutlined />}>续期</Button>
            </Dropdown>
          )}
        </Space>
      ) : (
        <Space size={4}>
          <Button size="small" icon={<PlayCircleOutlined />} onClick={() => handleRestart(record)}>重启</Button>
          <Popconfirm title="确定永久删除此记录?" onConfirm={() => handleHardDelete(record.id)}>
            <Button size="small" danger icon={<DeleteOutlined />}>删除</Button>
          </Popconfirm>
        </Space>
      ),
    },
  ];

  return (
    <div>
      <style>{`@keyframes blink { 50% { opacity: 0.5; } }`}</style>
      <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: 16 }}>
        <Title level={4} style={{ margin: 0 }}><SwapOutlined style={{ marginRight: 8 }} />端口转发</Title>
        <Space>
          <Button icon={<ReloadOutlined />} onClick={fetchTunnels}>刷新</Button>
          <Button icon={<GlobalOutlined />} onClick={() => setSocks5Visible(true)}>SOCKS5代理</Button>
          <Button type="primary" icon={<PlusOutlined />} onClick={() => setCreateVisible(true)}>创建隧道</Button>
        </Space>
      </div>

      {/* Tunnel Visualization Cards */}
      {activeTunnels.length > 0 && (
        <div style={{ marginBottom: 24 }}>
          <Text strong style={{ color: '#969696', fontSize: 12, textTransform: 'uppercase', letterSpacing: 1 }}>活跃隧道</Text>
          <Row gutter={[12, 12]} style={{ marginTop: 8 }}>
            {activeTunnels.map(t => (
              <Col span={8} key={t.id}>
                <Card size="small" style={{ borderColor: '#3c3c3c' }} hoverable>
                  <div style={{ display: 'flex', alignItems: 'center', gap: 8, justifyContent: 'space-between' }}>
                    <div style={{ flex: 1, minWidth: 0 }}>
                      <div style={{ fontSize: 11, color: '#969696', marginBottom: 4 }}>
                        {getDeviceName(t.device_id)}
                        {t.socks5_auth && <Tag color="orange" icon={<LockOutlined />} style={{ fontSize: 9, marginLeft: 4, padding: '0 4px' }}>认证</Tag>}
                      </div>
                      <div style={{ display: 'flex', alignItems: 'center', gap: 4, fontSize: 12 }}>
                        <Text code style={{ color: '#61afef', fontSize: 11 }}>{t.local_addr}</Text>
                        <ArrowRightOutlined style={{ color: '#52c41a', fontSize: 10 }} />
                        {t.tunnel_type === 'socks5' ?
                          <Tag color="purple" style={{ fontSize: 11, margin: 0 }}>SOCKS5</Tag> :
                          <Text code style={{ color: '#e5c07b', fontSize: 11 }}>{t.remote_addr}</Text>
                        }
                      </div>
                      <div style={{ fontSize: 11, color: '#666', marginTop: 4, display: 'flex', alignItems: 'center', gap: 8 }}>
                        <span>↑{(t.bytes_sent / 1024).toFixed(1)}KB ↓{(t.bytes_recv / 1024).toFixed(1)}KB</span>
                        {t.socks5_user && <span style={{ color: '#e5c07b' }}>用户: {t.socks5_user}</span>}
                        {t.allowed_ips && <span style={{ color: '#61afef' }}>IP: {t.allowed_ips}</span>}
                      </div>
                      {t.expires_at && (
                        <div style={{ fontSize: 11, marginTop: 4, display: 'flex', alignItems: 'center', gap: 4 }}>
                          <ClockCircleOutlined style={{ color: '#666', fontSize: 10 }} />
                          <CountdownTimer expiresAt={t.expires_at} onExpired={fetchTunnels} />
                        </div>
                      )}
                    </div>
                    <Space direction="vertical" size={4}>
                      {t.tunnel_type === 'socks5' && (
                        <Dropdown menu={{ items: renewMenuItems(t.id) }} trigger={['click']}>
                          <Button size="small" type="text" icon={<RetweetOutlined />} style={{ color: '#faad14' }} />
                        </Dropdown>
                      )}
                      <Popconfirm title="关闭隧道?" onConfirm={() => handleDelete(t.id)}>
                        <Button size="small" danger type="text" icon={<DisconnectOutlined />} />
                      </Popconfirm>
                    </Space>
                  </div>
                </Card>
              </Col>
            ))}
          </Row>
        </div>
      )}

      <Table columns={columns} dataSource={tunnels} rowKey="id" size="small"
        pagination={{ pageSize: 15, showTotal: (total) => `共 ${total} 条` }}
      />

      {/* Create Tunnel Modal */}
      <Modal title="创建端口转发" open={createVisible} onCancel={() => { setCreateVisible(false); form.resetFields(); }} onOk={() => form.submit()} width={480}>
        <Form form={form} onFinish={handleCreate} layout="vertical">
          <Form.Item name="device_id" label="目标设备" rules={[{ required: true, message: '请选择设备' }]}>
            <Select
              placeholder="选择设备"
              showSearch
              optionFilterProp="label"
              options={devices.filter(d => d.status === 'online').map(d => ({
                value: d.id,
                label: `${d.hostname} (${d.id.slice(0, 8)})`,
              }))}
              notFoundContent="暂无在线设备"
            />
          </Form.Item>
          <Form.Item name="remote_addr" label="远程地址" rules={[{ required: true, message: '请输入远程地址' }]}>
            <Input placeholder="例如: 127.0.0.1:8080" />
          </Form.Item>
          <div style={{ background: '#1e1e1e', padding: 12, borderRadius: 6, border: '1px solid #3c3c3c' }}>
            <Text type="secondary" style={{ fontSize: 12 }}>
              隧道创建后，本地将监听一个随机端口，所有到该端口的TCP连接将被转发到目标设备的远程地址。
            </Text>
            <div style={{ marginTop: 8, padding: '6px 8px', background: '#2a2a2a', borderRadius: 4, border: '1px solid #3c3c3c' }}>
              <Text type="warning" style={{ fontSize: 12 }}>
                此隧道将在 30 分钟后自动关闭。
              </Text>
            </div>
          </div>
        </Form>
      </Modal>

      {/* Create SOCKS5 Modal */}
      <Modal title="创建SOCKS5代理" open={socks5Visible} onCancel={() => { setSocks5Visible(false); socks5Form.resetFields(); }} onOk={() => socks5Form.submit()} width={480}>
        <Form form={socks5Form} onFinish={handleCreateSocks5} layout="vertical">
          <Form.Item name="device_id" label="目标设备" rules={[{ required: true, message: '请选择设备' }]}>
            <Select
              placeholder="选择设备"
              showSearch
              optionFilterProp="label"
              options={devices.filter(d => d.status === 'online').map(d => ({
                value: d.id,
                label: `${d.hostname} (${d.id.slice(0, 8)})`,
              }))}
              notFoundContent="暂无在线设备"
            />
          </Form.Item>
          <Form.Item name="socks5_user" label="认证用户名" tooltip="留空则启用无认证模式，任何人都可连接此代理">
            <Input placeholder="可选，留空则无需认证" prefix={<LockOutlined />} />
          </Form.Item>
          <Form.Item name="socks5_pass" label="认证密码" dependencies={['socks5_user']}>
            <Input.Password placeholder="可选，配合用户名使用" />
          </Form.Item>
          <Form.Item name="allowed_ips" label="允许的来源IP" tooltip="留空允许所有IP连接；填写IP或CIDR（逗号分隔），如: 127.0.0.1,192.168.1.0/24"
            rules={[{
              validator: (_: any, value: string) => {
                if (!value || !value.trim()) return Promise.resolve();
                const ipRe = /^(\d{1,3}\.){3}\d{1,3}$/;
                const cidrRe = /^(\d{1,3}\.){3}\d{1,3}\/\d{1,2}$/;
                const ipv6Re = /^([0-9a-fA-F]{0,4}:){2,7}[0-9a-fA-F]{0,4}(\/\d{1,3})?$/;
                for (const entry of value.split(',')) {
                  const e = entry.trim();
                  if (!e) continue;
                  if (!ipRe.test(e) && !cidrRe.test(e) && !ipv6Re.test(e)) {
                    return Promise.reject(new Error(`无效的IP或CIDR: ${e}`));
                  }
                }
                return Promise.resolve();
              }
            }]}
          >
            <Input placeholder="可选，如: 127.0.0.1,10.0.0.0/8" prefix={<GlobalOutlined />} />
          </Form.Item>
          <div style={{ background: '#1e1e1e', padding: 12, borderRadius: 6, border: '1px solid #3c3c3c' }}>
            <Text type="secondary" style={{ fontSize: 12 }}>
              SOCKS5代理创建后，服务器将监听一个本地端口作为SOCKS5代理入口。通过此代理的所有连接将通过目标设备的网络访问远程资源。
            </Text>
            <div style={{ marginTop: 8, padding: '6px 8px', background: '#2a2a2a', borderRadius: 4, border: '1px solid #3c3c3c' }}>
              <Text type="warning" style={{ fontSize: 12 }}>
                此代理将在 1 小时后自动关闭，届时可在隧道管理页面续期延长使用时间。
              </Text>
            </div>
            <div style={{ marginTop: 8, padding: '6px 8px', background: '#2a2a2a', borderRadius: 4, border: '1px solid #3c3c3c' }}>
              <Text type="secondary" style={{ fontSize: 12 }}>
                填写用户名和密码启用认证模式（推荐，浏览器插件需支持SOCKS5认证）；留空则启用无认证模式（兼容性更好，但安全性较低）。
              </Text>
            </div>
            <div style={{ marginTop: 8, padding: '6px 8px', background: '#2a2a2a', borderRadius: 4, border: '1px solid #3c3c3c' }}>
              <Text type="secondary" style={{ fontSize: 12 }}>
                IP白名单：限制哪些IP可以连接此代理。无认证模式下建议填写 127.0.0.1 仅允许本地连接，通过SSH隧道转发访问。云端部署时务必配置。
              </Text>
            </div>
          </div>
        </Form>
      </Modal>

      {/* SOCKS5 Creation Result Modal */}
      <Modal
        title="SOCKS5代理已创建"
        open={!!socks5Result}
        onCancel={() => setSocks5Result(null)}
        footer={<Button type="primary" onClick={() => setSocks5Result(null)}>我已保存凭据</Button>}
        width={500}
      >
        {socks5Result && (
          <div>
            <div style={{ marginBottom: 16 }}>
              <Text strong>代理地址</Text>
              <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginTop: 4 }}>
                <Text code style={{ fontSize: 14, color: '#61afef' }}>{socks5Result.local_addr}</Text>
                <Button size="small" icon={<CopyOutlined />} onClick={() => copyToClipboard(socks5Result.local_addr)}>复制</Button>
              </div>
            </div>
            {socks5Result.auth ? (
              <div>
                <div style={{ marginBottom: 12 }}>
                  <Text strong>认证信息</Text>
                  <Tag color="orange" icon={<LockOutlined />} style={{ marginLeft: 8 }}>已启用</Tag>
                </div>
                <div style={{ background: '#1e1e1e', padding: 12, borderRadius: 6, border: '1px solid #3c3c3c' }}>
                  <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 8 }}>
                    <Text type="secondary" style={{ width: 60 }}>用户名</Text>
                    <Text code>{socks5Result.socks5_user}</Text>
                    <Button size="small" icon={<CopyOutlined />} onClick={() => copyToClipboard(socks5Result.socks5_user)}>复制</Button>
                  </div>
                  <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                    <Text type="secondary" style={{ width: 60 }}>密码</Text>
                    <Text code>{socks5Result.socks5_pass}</Text>
                    <Button size="small" icon={<CopyOutlined />} onClick={() => copyToClipboard(socks5Result.socks5_pass)}>复制</Button>
                  </div>
                </div>
                <Text type="warning" style={{ fontSize: 12, display: 'block', marginTop: 8 }}>
                  请立即保存认证信息，密码仅在此处显示一次。
                </Text>
              </div>
            ) : (
              <div>
                <Tag color="default">未启用认证</Tag>
                <Text type="secondary" style={{ display: 'block', marginTop: 8, fontSize: 12 }}>
                  任何人都可以连接此代理，建议通过IP白名单限制访问来源。
                </Text>
              </div>
            )}
            {socks5Result.allowed_ips && (
              <div style={{ marginTop: 12 }}>
                <div style={{ marginBottom: 4 }}>
                  <Text strong>IP白名单</Text>
                  <Tag color="blue" icon={<GlobalOutlined />} style={{ marginLeft: 8 }}>已启用</Tag>
                </div>
                <div style={{ background: '#1e1e1e', padding: 8, borderRadius: 4, border: '1px solid #3c3c3c' }}>
                  <Text code style={{ fontSize: 13 }}>{socks5Result.allowed_ips}</Text>
                </div>
              </div>
            )}
            {socks5Result.expires_at && (
              <div style={{ marginTop: 12, padding: '8px 12px', background: '#1e1e1e', borderRadius: 6, border: '1px solid #3c3c3c' }}>
                <Space>
                  <ClockCircleOutlined style={{ color: '#faad14' }} />
                  <Text type="warning" style={{ fontSize: 12 }}>
                    代理将在约1小时后自动关闭，可在隧道管理页面续期。
                  </Text>
                </Space>
              </div>
            )}
            <div style={{ marginTop: 16, background: '#1e1e1e', padding: 12, borderRadius: 6, border: '1px solid #3c3c3c' }}>
              <Text type="secondary" style={{ fontSize: 12 }}>
                客户端配置示例:
              </Text>
              <pre style={{ margin: '8px 0 0', fontSize: 12, color: '#abb2bf' }}>
{socks5Result.auth
  ? `curl -x socks5h://${socks5Result.socks5_user}:${socks5Result.socks5_pass}@${socks5Result.local_addr} https://example.com`
  : `curl -x socks5h://${socks5Result.local_addr} https://example.com`
}
              </pre>
            </div>
          </div>
        )}
      </Modal>
    </div>
  );
}
