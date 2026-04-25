import { useEffect, useState, useCallback } from 'react';
import { Table, Input, Typography, Select, DatePicker, Button, Space, Tag, message } from 'antd';
import { SearchOutlined, ExportOutlined, ReloadOutlined } from '@ant-design/icons';
import apiClient from '../api/client';
import type { AuditEntry } from '../api/types';

const { Title } = Typography;
const { RangePicker } = DatePicker;

const actionLabels: Record<string, string> = {
  'shell.start': '终端启动',
  'file.write': '文件写入',
  'file.upload': '文件上传',
  'file.download': '文件下载',
  'process.kill': '进程终止',
  'tunnel.create': '隧道创建',
  'tunnel.close': '隧道关闭',
  'settings.update': '设置更新',
  'device.delete': '设备删除',
};

const actionColors: Record<string, string> = {
  'shell.start': 'blue',
  'file.write': 'green',
  'file.upload': 'cyan',
  'file.download': 'geekblue',
  'process.kill': 'red',
  'tunnel.create': 'purple',
  'tunnel.close': 'orange',
  'settings.update': 'gold',
  'device.delete': 'volcano',
};

export default function AuditLogs() {
  const [logs, setLogs] = useState<AuditEntry[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(false);
  const [page, setPage] = useState(1);
  const [actionFilter, setActionFilter] = useState<string>('');
  const [dateRange, setDateRange] = useState<[string, string] | null>(null);

  const fetchLogs = useCallback(async (p = 1) => {
    setLoading(true);
    try {
      const params: any = { limit: 20, offset: (p - 1) * 20 };
      if (actionFilter) params.action = actionFilter;
      if (dateRange) {
        params.from = dateRange[0];
        params.to = dateRange[1];
      }
      const { data } = await apiClient.get('/audit-logs', { params });
      setLogs(data.items || []);
      setTotal(data.total || 0);
    } catch { /* ignore */ }
    setLoading(false);
  }, [actionFilter, dateRange]);

  useEffect(() => { fetchLogs(); }, [fetchLogs]);

  const handleExport = () => {
    const csv = [
      ['ID', '用户ID', '设备ID', '操作', '资源', '来源IP', '时间'].join(','),
      ...logs.map(l => [l.id, l.user_id, l.device_id || '', l.action, l.resource || '', l.source_ip || '', l.created_at].join(','))
    ].join('\n');
    const blob = new Blob(['\uFEFF' + csv], { type: 'text/csv;charset=utf-8;' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `audit-logs-${new Date().toISOString().slice(0, 10)}.csv`;
    a.click();
    URL.revokeObjectURL(url);
    message.success('导出成功');
  };

  const columns = [
    { title: 'ID', dataIndex: 'id', key: 'id', width: 60 },
    { title: '用户', dataIndex: 'user_id', key: 'user_id', width: 100, render: (v: string) => v?.slice(0, 8) || '-' },
    { title: '设备', dataIndex: 'device_id', key: 'device_id', width: 100, render: (v: string) => v?.slice(0, 8) || '-' },
    { title: '操作', dataIndex: 'action', key: 'action', width: 140, render: (v: string) => (
      <Tag color={actionColors[v] || 'default'}>{actionLabels[v] || v}</Tag>
    )},
    { title: '资源', dataIndex: 'resource', key: 'resource', ellipsis: true },
    { title: '来源IP', dataIndex: 'source_ip', key: 'source_ip', width: 130 },
    { title: '时间', dataIndex: 'created_at', key: 'created_at', width: 180 },
  ];

  return (
    <div>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 16 }}>
        <Title level={4} style={{ margin: 0 }}>审计日志</Title>
        <Space>
          <Button icon={<ExportOutlined />} size="small" onClick={handleExport}>导出CSV</Button>
        </Space>
      </div>

      <div style={{ display: 'flex', gap: 12, marginBottom: 16, flexWrap: 'wrap' }}>
        <Select
          placeholder="操作类型"
          allowClear
          style={{ width: 160 }}
          value={actionFilter || undefined}
          onChange={(v) => { setActionFilter(v || ''); setPage(1); }}
          options={Object.entries(actionLabels).map(([k, v]) => ({ value: k, label: v }))}
        />
        <RangePicker
          style={{ minWidth: 240 }}
          onChange={(_, dateStrings) => {
            if (dateStrings[0] && dateStrings[1]) {
              setDateRange([dateStrings[0], dateStrings[1]]);
            } else {
              setDateRange(null);
            }
            setPage(1);
          }}
        />
        <Button icon={<ReloadOutlined />} onClick={() => fetchLogs(page)}>刷新</Button>
      </div>

      <Table
        columns={columns}
        dataSource={logs}
        rowKey="id"
        size="small"
        loading={loading}
        pagination={{ current: page, total, pageSize: 20, showTotal: (t) => `共 ${t} 条`, onChange: (p) => { setPage(p); fetchLogs(p); } }}
      />
    </div>
  );
}
