import { useEffect, useState, useCallback } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { Card, Descriptions, Tag, Button, Typography, message, Table, Space, Modal, Form, Input, Popconfirm, Drawer, Dropdown } from 'antd';
import { ArrowLeftOutlined, ReloadOutlined, SearchOutlined, InfoCircleOutlined, AppstoreOutlined, SwapOutlined, DashboardOutlined, FieldTimeOutlined, DeleteOutlined, PlayCircleOutlined, ClockCircleOutlined, EditOutlined, PlusOutlined } from '@ant-design/icons';
import apiClient from '../api/client';
import type { Device, ProcessInfo, Tunnel as TunnelType } from '../api/types';
import Workbench from '../components/Workbench/Workbench';
import MonitoringPanel from '../components/Workbench/MonitoringPanel';
import { InlineUploadProgress } from '../components/Layout/UploadProgressPanel';
import { useDeviceStore } from '../store/deviceSlice';

const { Title, Text } = Typography;

export default function DeviceDetail() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const updateDeviceNotes = useDeviceStore((s) => s.updateDeviceNotes);
  const [device, setDevice] = useState<Device | null>(null);
  const [processes, setProcesses] = useState<ProcessInfo[]>([]);
  const [processSearch, setProcessSearch] = useState('');
  const [tunnels, setTunnels] = useState<TunnelType[]>([]);
  const [loading, setLoading] = useState(false);
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [drawerContent, setDrawerContent] = useState<'overview' | 'processes' | 'tunnels' | 'monitor'>('overview');
  const [editingNotes, setEditingNotes] = useState(false);

  const fetchDevice = async () => {
    if (!id) return;
    setLoading(true);
    try {
      const { data } = await apiClient.get<Device>(`/devices/${id}`);
      setDevice(data);
    } catch {
      message.error('获取设备信息失败');
    } finally {
      setLoading(false);
    }
  };

  const fetchProcesses = async () => {
    if (!id) return;
    try {
      const { data } = await apiClient.get(`/devices/${id}/processes`);
      setProcesses(data.processes || []);
    } catch {
      message.error('获取进程列表失败');
    }
  };

  const fetchTunnels = async () => {
    try {
      const { data } = await apiClient.get<TunnelType[]>('/tunnels');
      setTunnels((data || []).filter((t: TunnelType) => t.device_id === id));
    } catch {
      // ignore
    }
  };

  useEffect(() => { fetchDevice(); }, [id]);

  const handleKillProcess = async (pid: number) => {
    if (!id) return;
    try {
      await apiClient.post(`/devices/${id}/processes/${pid}/kill`, { pid });
      message.success(`进程 ${pid} 已终止`);
      fetchProcesses();
    } catch {
      message.error('终止进程失败');
    }
  };

  const filteredProcesses = processes.filter(p => {
    if (!processSearch) return true;
    const q = processSearch.toLowerCase();
    return p.name.toLowerCase().includes(q) || String(p.pid).includes(q);
  });

  const processColumns = [
    { title: 'PID', dataIndex: 'pid', key: 'pid', width: 80, sorter: (a: ProcessInfo, b: ProcessInfo) => a.pid - b.pid },
    { title: '名称', dataIndex: 'name', key: 'name', sorter: (a: ProcessInfo, b: ProcessInfo) => a.name.localeCompare(b.name) },
    { title: 'CPU%', dataIndex: 'cpu', key: 'cpu', width: 100, render: (v: number) => v.toFixed(1), sorter: (a: ProcessInfo, b: ProcessInfo) => a.cpu - b.cpu },
    { title: 'MEM%', dataIndex: 'mem', key: 'mem', width: 100, render: (v: number) => v.toFixed(1), sorter: (a: ProcessInfo, b: ProcessInfo) => a.mem - b.mem },
    { title: '状态', dataIndex: 'status', key: 'status', width: 80 },
    {
      title: '操作', key: 'action', width: 80,
      render: (_: any, record: ProcessInfo) => (
        <Popconfirm title={`确定终止进程 ${record.pid}?`} onConfirm={() => handleKillProcess(record.pid)}>
          <Button size="small" danger>终止</Button>
        </Popconfirm>
      ),
    },
  ];

  const renewMenuItems = (tunnelId: string) => [
    { key: '1', label: '+1小时', onClick: () => handleRenew(tunnelId, 1) },
    { key: '2', label: '+2小时', onClick: () => handleRenew(tunnelId, 2) },
    { key: '3', label: '+3小时', onClick: () => handleRenew(tunnelId, 3) },
    { key: '4', label: '+4小时', onClick: () => handleRenew(tunnelId, 4) },
    { key: '5', label: '+5小时', onClick: () => handleRenew(tunnelId, 5) },
  ];

  const handleRenew = async (tunnelId: string, hours: number) => {
    try {
      await apiClient.put(`/tunnels/${tunnelId}/renew`, { duration_hours: hours });
      message.success(`已续期 ${hours} 小时`);
      fetchTunnels();
    } catch (e: any) {
      message.error(e.response?.data?.error || '续期失败');
    }
  };

  const tunnelColumns = [
    { title: 'ID', dataIndex: 'id', key: 'id', render: (v: string) => v.slice(0, 8) },
    { title: '本地地址', dataIndex: 'local_addr', key: 'local_addr' },
    { title: '远程地址', dataIndex: 'remote_addr', key: 'remote_addr', render: (v: string, record: TunnelType) =>
      record.tunnel_type === 'socks5' ? <Tag color="purple">SOCKS5</Tag> : v
    },
    {
      title: '流量', key: 'traffic',
      render: (_: any, record: TunnelType) => `${(record.bytes_sent / 1024).toFixed(1)}KB / ${(record.bytes_recv / 1024).toFixed(1)}KB`,
    },
    {
      title: '倒计时', key: 'countdown',
      render: (_: any, record: TunnelType) => record.state === 'active' && record.expires_at ? (
        <span style={{ fontSize: 11, fontFamily: 'monospace', color: '#faad14' }}>
          <ClockCircleOutlined style={{ marginRight: 4 }} />
          {Math.max(0, Math.floor((new Date(record.expires_at).getTime() - Date.now()) / 60000))}分钟
        </span>
      ) : <Text type="secondary">-</Text>,
    },
    {
      title: '操作', key: 'action',
      render: (_: any, record: TunnelType) => record.state === 'active' ? (
        <Space size={4}>
          <Popconfirm title="确定关闭隧道?" onConfirm={async () => {
            await apiClient.delete(`/tunnels/${record.id}`);
            message.success('隧道已关闭');
            fetchTunnels();
          }}>
            <Button size="small" danger>关闭</Button>
          </Popconfirm>
          {record.tunnel_type === 'socks5' && (
            <Dropdown menu={{ items: renewMenuItems(record.id) }} trigger={['click']}>
              <Button size="small" icon={<FieldTimeOutlined />}>续期</Button>
            </Dropdown>
          )}
        </Space>
      ) : (
        <Space size={4}>
          <Button size="small" icon={<PlayCircleOutlined />} onClick={() => {
            if (record.tunnel_type === 'socks5') {
              message.info('请在隧道管理页面创建新的SOCKS5代理');
            } else {
              message.info('请在隧道管理页面重新创建端口转发');
            }
          }}>重启</Button>
          <Popconfirm title="确定永久删除此记录?" onConfirm={async () => {
            try {
              await apiClient.delete(`/tunnels/${record.id}/permanent`);
              message.success('记录已删除');
              fetchTunnels();
            } catch (e: any) {
              message.error(e.response?.data?.error || '删除失败');
            }
          }}>
            <Button size="small" danger icon={<DeleteOutlined />}>删除</Button>
          </Popconfirm>
        </Space>
      ),
    },
  ];

  const CreateTunnelForm = () => {
    const [form] = Form.useForm();
    return (
      <Form form={form} onFinish={async (values: { remote_addr: string }) => {
        try {
          await apiClient.post('/tunnels', { device_id: id, remote_addr: values.remote_addr });
          message.success('隧道创建成功');
          form.resetFields();
          fetchTunnels();
        } catch {
          message.error('创建隧道失败');
        }
      }} layout="inline">
        <Form.Item name="remote_addr" rules={[{ required: true, message: '请输入远程地址' }]}>
          <Input placeholder="127.0.0.1:8080" style={{ width: 200 }} />
        </Form.Item>
        <Button type="primary" htmlType="submit">创建</Button>
      </Form>
    );
  };

  const openDrawer = (content: 'overview' | 'processes' | 'tunnels' | 'monitor') => {
    setDrawerContent(content);
    setDrawerOpen(true);
    if (content === 'processes') fetchProcesses();
    if (content === 'tunnels') fetchTunnels();
  };

  if (!device) return <div>加载中...</div>;

  return (
    <div style={{ height: '100%', display: 'flex', flexDirection: 'column' }}>
      {/* Header bar */}
      <div style={{ display: 'flex', alignItems: 'center', padding: '8px 16px', background: '#1e1e1e', borderBottom: '1px solid #3c3c3c', flexShrink: 0 }}>
        <Button icon={<ArrowLeftOutlined />} onClick={() => navigate('/devices')} style={{ marginRight: 12 }} type="text" />
        <Title level={5} style={{ margin: 0, color: '#cccccc' }}>{device.notes || device.hostname || device.id}</Title>
        {device.notes && <Text type="secondary" style={{ marginLeft: 8, fontSize: 12 }}>{device.hostname}</Text>}
        <Tag color={device.status === 'online' ? 'green' : 'red'} style={{ marginLeft: 12 }}>
          {device.status === 'online' ? '在线' : '离线'}
        </Tag>
        <div style={{ flex: 1 }} />
        <Space>
          <InlineUploadProgress deviceId={id} />
          <Button size="small" icon={<InfoCircleOutlined />} onClick={() => openDrawer('overview')}>概览</Button>
          <Button size="small" icon={<DashboardOutlined />} onClick={() => openDrawer('monitor')}>监控</Button>
          <Button size="small" icon={<AppstoreOutlined />} onClick={() => openDrawer('processes')}>进程</Button>
          <Button size="small" icon={<SwapOutlined />} onClick={() => openDrawer('tunnels')}>隧道</Button>
        </Space>
      </div>

      {/* Main Workbench */}
      <div style={{ flex: 1, overflow: 'hidden' }}>
        {device.status === 'online' ? (
          <Workbench deviceId={device.id} />
        ) : (
          <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '100%', color: '#666' }}>
            设备离线，无法打开工作台
          </div>
        )}
      </div>

      {/* Side drawer for overview/processes/tunnels */}
      <Drawer
        title={drawerContent === 'overview' ? '系统概览' : drawerContent === 'processes' ? '进程管理' : drawerContent === 'monitor' ? '实时监控' : '端口转发'}
        placement="right"
        width={520}
        open={drawerOpen}
        onClose={() => setDrawerOpen(false)}
        styles={{ body: { padding: 16 } }}
      >
        {drawerContent === 'overview' && (
          <Card size="small">
            <Descriptions column={1} size="small">
              <Descriptions.Item label="主机名">{device.hostname}</Descriptions.Item>
              <Descriptions.Item label="备注">
                {editingNotes ? (
                  <Input.TextArea
                    defaultValue={device.notes || ''}
                    autoSize={{ minRows: 1, maxRows: 4 }}
                    autoFocus
                    style={{ fontSize: 13 }}
                    onBlur={async (e) => {
                      const val = e.target.value;
                      setEditingNotes(false);
                      if (val !== (device.notes || '')) {
                        try {
                          await updateDeviceNotes(id!, val);
                          setDevice({ ...device, notes: val });
                          message.success('备注已更新');
                        } catch {
                          message.error('更新备注失败');
                        }
                      }
                    }}
                    onKeyDown={(e) => {
                      if (e.key === 'Escape') setEditingNotes(false);
                      e.stopPropagation();
                    }}
                    onClick={(e) => e.stopPropagation()}
                    onMouseDown={(e) => e.stopPropagation()}
                  />
                ) : (
                  <span
                    style={{ cursor: 'pointer', display: 'inline-flex', alignItems: 'center', gap: 6 }}
                    onClick={() => setEditingNotes(true)}
                  >
                    {device.notes ? (
                      <>
                        <Text>{device.notes}</Text>
                        <EditOutlined style={{ fontSize: 12, color: '#888' }} />
                      </>
                    ) : (
                      <Text type="secondary" style={{ fontSize: 12 }}>
                        <PlusOutlined style={{ marginRight: 4 }} />点击添加备注
                      </Text>
                    )}
                  </span>
                )}
              </Descriptions.Item>
              <Descriptions.Item label="操作系统">{device.os}</Descriptions.Item>
              <Descriptions.Item label="架构">{device.arch}</Descriptions.Item>
              <Descriptions.Item label="内核">{device.kernel}</Descriptions.Item>
              <Descriptions.Item label="出口IP">
                <Space size={4}>
                  <Text code>{device.ip || 'N/A'}</Text>
                  {device.ip_location && <Tag color="blue" style={{ fontSize: 11 }}>{device.ip_location}</Tag>}
                </Space>
              </Descriptions.Item>
              {device.ip_internal && <Descriptions.Item label="内网IP"><Text code>{device.ip_internal}</Text></Descriptions.Item>}
              <Descriptions.Item label="Agent版本">{device.agent_version}</Descriptions.Item>
              <Descriptions.Item label="最后在线">{device.last_seen_at || 'N/A'}</Descriptions.Item>
              <Descriptions.Item label="注册时间">{device.registered_at}</Descriptions.Item>
            </Descriptions>
          </Card>
        )}
        {drawerContent === 'monitor' && (
          <MonitoringPanel deviceId={device.id} />
        )}
        {drawerContent === 'processes' && (
          <div>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 12 }}>
              <Input
                placeholder="搜索进程名称或PID"
                prefix={<SearchOutlined />}
                value={processSearch}
                onChange={e => setProcessSearch(e.target.value)}
                allowClear
                style={{ width: 240 }}
                size="small"
              />
              <Button size="small" icon={<ReloadOutlined />} onClick={fetchProcesses}>刷新</Button>
            </div>
            <Table
              columns={processColumns}
              dataSource={filteredProcesses}
              rowKey="pid"
              size="small"
              pagination={{ pageSize: 15, showSizeChanger: true, showTotal: (total) => `共 ${total} 条` }}
            />
          </div>
        )}
        {drawerContent === 'tunnels' && (
          <div>
            <CreateTunnelForm />
            <Table
              columns={tunnelColumns}
              dataSource={tunnels}
              rowKey="id"
              size="small"
              pagination={false}
              style={{ marginTop: 12 }}
            />
          </div>
        )}
      </Drawer>
    </div>
  );
}
