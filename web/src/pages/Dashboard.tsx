import { useEffect } from 'react';
import { Row, Col, Card, Statistic, Typography, Table } from 'antd';
import { DesktopOutlined, CheckCircleOutlined, CloseCircleOutlined, GlobalOutlined } from '@ant-design/icons';
import { useDeviceStore } from '../store/deviceSlice';
import { useNavigate } from 'react-router-dom';

const { Title, Text } = Typography;

export default function Dashboard() {
  const { devices, fetchDevices } = useDeviceStore();
  const navigate = useNavigate();

  useEffect(() => {
    fetchDevices();
    const interval = setInterval(fetchDevices, 10000);
    return () => clearInterval(interval);
  }, [fetchDevices]);

  const online = devices.filter((d) => d.status === 'online').length;
  const offline = devices.filter((d) => d.status === 'offline').length;
  const onlineDevices = devices.filter((d) => d.status === 'online');

  const ipColumns = [
    {
      title: '主机名',
      dataIndex: 'hostname',
      key: 'hostname',
      render: (v: string, record: any) => v || record.id.slice(0, 12),
    },
    {
      title: 'IP 地址',
      dataIndex: 'ip',
      key: 'ip',
      render: (v: string) => (
        <Text code style={{ fontSize: 13 }}>{v || 'N/A'}</Text>
      ),
    },
    {
      title: '系统',
      dataIndex: 'os',
      key: 'os',
      render: (v: string, record: any) => `${v} / ${record.arch}`,
    },
    {
      title: '状态',
      dataIndex: 'status',
      key: 'status',
      width: 80,
      render: (v: string) => (
        <span style={{ color: v === 'online' ? '#52c41a' : '#ff4d4f' }}>
          {v === 'online' ? '在线' : '离线'}
        </span>
      ),
    },
  ];

  return (
    <div>
      <Title level={4}>系统概览</Title>
      <Row gutter={[16, 16]} style={{ marginBottom: 24 }}>
        <Col span={8}>
          <Card hoverable onClick={() => navigate('/devices')}>
            <Statistic title="设备总数" value={devices.length} prefix={<DesktopOutlined />} />
          </Card>
        </Col>
        <Col span={8}>
          <Card hoverable onClick={() => navigate('/devices?status=online')}>
            <Statistic title="在线设备" value={online} prefix={<CheckCircleOutlined />} valueStyle={{ color: '#52c41a' }} />
          </Card>
        </Col>
        <Col span={8}>
          <Card hoverable onClick={() => navigate('/devices?status=offline')}>
            <Statistic title="离线设备" value={offline} prefix={<CloseCircleOutlined />} valueStyle={{ color: '#ff4d4f' }} />
          </Card>
        </Col>
      </Row>

      <Title level={4}><GlobalOutlined style={{ marginRight: 8 }} />在线设备 IP</Title>
      <Card style={{ marginBottom: 24 }}>
        <Table
          columns={ipColumns}
          dataSource={onlineDevices}
          rowKey="id"
          size="small"
          pagination={false}
          onRow={(record) => ({
            onClick: () => navigate(`/devices/${record.id}`),
            style: { cursor: 'pointer' },
          })}
          locale={{ emptyText: '暂无在线设备' }}
        />
      </Card>

      <Title level={4}>最近设备</Title>
      <Row gutter={[16, 16]}>
        {devices.slice(0, 6).map((device) => (
          <Col span={8} key={device.id}>
            <Card
              hoverable
              size="small"
              onClick={() => navigate(`/devices/${device.id}`)}
              extra={<span style={{ color: device.status === 'online' ? '#52c41a' : '#ff4d4f' }}>{device.status === 'online' ? '在线' : '离线'}</span>}
            >
              <Card.Meta
                title={device.hostname || device.id.slice(0, 8)}
                description={
                  <div>
                    <div>{device.os} / {device.arch}</div>
                    <div style={{ fontFamily: 'monospace', fontSize: 12, marginTop: 2 }}>IP: {device.ip || 'N/A'}</div>
                  </div>
                }
              />
            </Card>
          </Col>
        ))}
      </Row>
    </div>
  );
}
