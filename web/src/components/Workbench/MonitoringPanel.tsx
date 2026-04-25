import { useState, useEffect, useRef, useCallback } from 'react';
import { Typography, Row, Col } from 'antd';
import wsManager from '../../api/ws';
import apiClient from '../../api/client';

const { Text } = Typography;

interface MetricPoint {
  ts: number;
  value: number;
}

interface MetricsData {
  cpu: MetricPoint[];
  memory: MetricPoint[];
  disk: MetricPoint[];
  load1: number;
  load5: number;
  load15: number;
  uptime: number;
  lastUpdate: number;
}

const MAX_POINTS = 60;

interface Props {
  deviceId: string;
}

function MiniChart({ data, color, label, unit, maxValue }: {
  data: MetricPoint[];
  color: string;
  label: string;
  unit: string;
  maxValue: number;
}) {
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const latest = data.length > 0 ? data[data.length - 1].value : 0;

  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;
    const ctx = canvas.getContext('2d');
    if (!ctx) return;

    const w = canvas.width;
    const h = canvas.height;
    ctx.clearRect(0, 0, w, h);

    if (data.length < 2) return;

    ctx.strokeStyle = '#333';
    ctx.lineWidth = 0.5;
    for (let i = 1; i < 4; i++) {
      const y = (h / 4) * i;
      ctx.beginPath();
      ctx.moveTo(0, y);
      ctx.lineTo(w, y);
      ctx.stroke();
    }

    const stepX = w / (MAX_POINTS - 1);
    const startIdx = Math.max(0, data.length - MAX_POINTS);
    const visibleData = data.slice(startIdx);

    ctx.beginPath();
    ctx.moveTo(0, h);
    visibleData.forEach((point, i) => {
      const x = i * stepX;
      const y = h - (point.value / maxValue) * h;
      ctx.lineTo(x, y);
    });
    ctx.lineTo((visibleData.length - 1) * stepX, h);
    ctx.closePath();

    const gradient = ctx.createLinearGradient(0, 0, 0, h);
    gradient.addColorStop(0, color + '40');
    gradient.addColorStop(1, color + '05');
    ctx.fillStyle = gradient;
    ctx.fill();

    ctx.beginPath();
    visibleData.forEach((point, i) => {
      const x = i * stepX;
      const y = h - (point.value / maxValue) * h;
      if (i === 0) ctx.moveTo(x, y);
      else ctx.lineTo(x, y);
    });
    ctx.strokeStyle = color;
    ctx.lineWidth = 1.5;
    ctx.stroke();
  }, [data, color, maxValue]);

  return (
    <div style={{ background: '#1a1a1a', borderRadius: 6, padding: 10, border: '1px solid #2a2a2a' }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: 6 }}>
        <Text style={{ fontSize: 11, color: '#999' }}>{label}</Text>
        <Text style={{ fontSize: 14, color, fontWeight: 600 }}>{latest.toFixed(1)}{unit}</Text>
      </div>
      <canvas ref={canvasRef} width={220} height={60} style={{ width: '100%', height: 60 }} />
    </div>
  );
}

function formatUptime(seconds: number): string {
  if (!seconds) return 'N/A';
  const days = Math.floor(seconds / 86400);
  const hours = Math.floor((seconds % 86400) / 3600);
  const mins = Math.floor((seconds % 3600) / 60);
  if (days > 0) return `${days}天 ${hours}小时`;
  if (hours > 0) return `${hours}小时 ${mins}分钟`;
  return `${mins}分钟`;
}

export default function MonitoringPanel({ deviceId }: Props) {
  const [metrics, setMetrics] = useState<MetricsData>({
    cpu: [],
    memory: [],
    disk: [],
    load1: 0,
    load5: 0,
    load15: 0,
    uptime: 0,
    lastUpdate: 0,
  });

  const handleHeartbeat = useCallback((env: any) => {
    const p = env.payload;
    if (!p) return;
    // Accept heartbeats from the device we're monitoring, or without device_id (legacy)
    if (p.device_id && p.device_id !== deviceId) return;
    const now = Date.now();

    setMetrics(prev => {
      const addPoint = (arr: MetricPoint[], value: number): MetricPoint[] => {
        const next = [...arr, { ts: now, value }];
        return next.length > MAX_POINTS ? next.slice(-MAX_POINTS) : next;
      };
      return {
        cpu: addPoint(prev.cpu, p.cpu || 0),
        memory: addPoint(prev.memory, p.mem || p.memory || 0),
        disk: addPoint(prev.disk, p.disk || 0),
        load1: p.load1 || 0,
        load5: p.load5 || 0,
        load15: p.load15 || 0,
        uptime: p.uptime || 0,
        lastUpdate: now,
      };
    });
  }, [deviceId]);

  // Listen for heartbeat events via WebSocket
  useEffect(() => {
    wsManager.on('system.heartbeat', handleHeartbeat);
    return () => { wsManager.off('system.heartbeat', handleHeartbeat); };
  }, [handleHeartbeat]);

  // On mount, request initial system info via REST API as a fallback
  // This gives immediate data instead of waiting for the first heartbeat
  useEffect(() => {
    const fetchSystemInfo = async () => {
      try {
        const { data } = await apiClient.get(`/devices/${deviceId}/system`);
        if (!data) return;
        const now = Date.now();
        setMetrics(prev => {
          const addPoint = (arr: MetricPoint[], value: number): MetricPoint[] => {
            if (arr.length > 0 && now - arr[arr.length - 1].ts < 5000) return arr; // Don't duplicate if heartbeat already came
            const next = [...arr, { ts: now, value }];
            return next.length > MAX_POINTS ? next.slice(-MAX_POINTS) : next;
          };
          return {
            cpu: addPoint(prev.cpu, data.cpu_percent || 0),
            memory: addPoint(prev.memory, data.memory?.used_percent || 0),
            disk: addPoint(prev.disk, data.disks?.[0]?.used_percent || 0),
            load1: data.load1 || 0,
            load5: data.load5 || 0,
            load15: data.load15 || 0,
            uptime: data.uptime || 0,
            lastUpdate: prev.lastUpdate || now,
          };
        });
      } catch {
        // System info endpoint may not exist yet; ignore
      }
    };
    fetchSystemInfo();
  }, [deviceId]);

  const cpuColor = metrics.cpu.length > 0 ? (metrics.cpu[metrics.cpu.length - 1].value > 80 ? '#ff4d4f' : metrics.cpu[metrics.cpu.length - 1].value > 50 ? '#faad14' : '#52c41a') : '#52c41a';
  const memColor = metrics.memory.length > 0 ? (metrics.memory[metrics.memory.length - 1].value > 85 ? '#ff4d4f' : metrics.memory[metrics.memory.length - 1].value > 60 ? '#faad14' : '#1890ff') : '#1890ff';

  return (
    <div>
      <Row gutter={[8, 8]} style={{ marginBottom: 12 }}>
        <Col span={12}>
          <MiniChart data={metrics.cpu} color={cpuColor} label="CPU 使用率" unit="%" maxValue={100} />
        </Col>
        <Col span={12}>
          <MiniChart data={metrics.memory} color={memColor} label="内存 使用率" unit="%" maxValue={100} />
        </Col>
      </Row>
      <Row gutter={[8, 8]} style={{ marginBottom: 12 }}>
        <Col span={12}>
          <MiniChart data={metrics.disk} color="#faad14" label="磁盘 使用率" unit="%" maxValue={100} />
        </Col>
        <Col span={12}>
          <div style={{ background: '#1a1a1a', borderRadius: 6, padding: 10, border: '1px solid #2a2a2a' }}>
            <Text style={{ fontSize: 11, color: '#999', display: 'block', marginBottom: 6 }}>系统负载</Text>
            <div style={{ display: 'flex', gap: 12 }}>
              <div>
                <Text style={{ fontSize: 10, color: '#666' }}>1min</Text>
                <div style={{ fontSize: 14, color: '#e5c07b', fontWeight: 600 }}>{metrics.load1.toFixed(2)}</div>
              </div>
              <div>
                <Text style={{ fontSize: 10, color: '#666' }}>5min</Text>
                <div style={{ fontSize: 14, color: '#e5c07b', fontWeight: 600 }}>{metrics.load5.toFixed(2)}</div>
              </div>
              <div>
                <Text style={{ fontSize: 10, color: '#666' }}>15min</Text>
                <div style={{ fontSize: 14, color: '#e5c07b', fontWeight: 600 }}>{metrics.load15.toFixed(2)}</div>
              </div>
            </div>
            <div style={{ marginTop: 8 }}>
              <Text style={{ fontSize: 10, color: '#666' }}>运行时间</Text>
              <div style={{ fontSize: 12, color: '#999' }}>{formatUptime(metrics.uptime)}</div>
            </div>
          </div>
        </Col>
      </Row>
      <div style={{ textAlign: 'center' }}>
        <Text style={{ fontSize: 10, color: '#555' }}>
          {metrics.lastUpdate ? `最后更新: ${new Date(metrics.lastUpdate).toLocaleTimeString('zh-CN')}` : '等待心跳数据...'}
        </Text>
      </div>
    </div>
  );
}
