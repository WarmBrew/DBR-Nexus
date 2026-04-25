import { useEffect, useState, useMemo } from 'react';
import { useNavigate } from 'react-router-dom';
import { Row, Col, Card, Tag, Input, Typography, message, theme, Button, Modal, Select, Form, Tabs, Tooltip } from 'antd';
import { DesktopOutlined, WindowsOutlined, AppleOutlined, LinuxOutlined, EditOutlined, DownloadOutlined, CopyOutlined } from '@ant-design/icons';
import { useDeviceStore } from '../store/deviceSlice';
import { useAuthStore } from '../store/authSlice';
import apiClient from '../api/client';
import { buildAndDownloadAgent } from '../api/agent';

const { Title } = Typography;
const { Search } = Input;

function OsIcon({ os }: { os: string }) {
  const lower = os.toLowerCase();
  if (lower.includes('windows')) return <WindowsOutlined />;
  if (lower.includes('darwin') || lower.includes('mac')) return <AppleOutlined />;
  if (lower.includes('linux') || lower.includes('ubuntu') || lower.includes('centos')) return <LinuxOutlined />;
  return <DesktopOutlined />;
}

export default function Devices() {
  const { devices, loading, fetchDevices } = useDeviceStore();
  const navigate = useNavigate();
  const { token: themeToken } = theme.useToken();
  const [downloadOpen, setDownloadOpen] = useState(false);
  const [downloading, setDownloading] = useState(false);
  const [form] = Form.useForm();
  const [editingNotes, setEditingNotes] = useState<Record<string, string>>({});

  // Watch form values for generating install commands
  const platform = Form.useWatch('platform', form) || 'windows';
  const arch = Form.useWatch('arch', form) || 'amd64';
  const serverUrl = Form.useWatch('server_url', form) || '';
  const psk = Form.useWatch('psk', form) || '';
  const authToken = useAuthStore((s) => s.token);

  // Generate quick install commands based on current form values
  const installCommands = useMemo(() => {
    const baseUrl = `${window.location.protocol}//${serverUrl || window.location.host}`;
    const downloadUrl = `${baseUrl}/api/v1/agent/download?platform=${platform}&arch=${arch}&server_url=${encodeURIComponent(serverUrl || window.location.host)}${psk ? '&psk=' + encodeURIComponent(psk) : ''}`;
    const authHeader = authToken ? `-H "Authorization: Bearer ${authToken}"` : '';

    if (platform === 'linux' || platform === 'darwin') {
      const binName = 'qoder-agent';
      return {
        curl: `curl -sL ${authHeader} "${downloadUrl}" -o /tmp/${binName} && chmod +x /tmp/${binName} && /tmp/${binName}`,
        wget: `wget -q ${authHeader ? '--header="Authorization: Bearer ' + authToken + '" ' : ''}"${downloadUrl}" -O /tmp/${binName} && chmod +x /tmp/${binName} && /tmp/${binName}`,
      };
    }
    // Windows
    const binName = 'qoder-agent.exe';
    const tmpDir = '$env:TEMP';
    const psCmd = authToken
      ? `powershell -Command "$h=@{Authorization='Bearer ${authToken}'}; Invoke-WebRequest -Uri '${downloadUrl}' -Headers $h -OutFile ${tmpDir}\\${binName}; & '${tmpDir}\\${binName}'"`
      : `powershell -Command "Invoke-WebRequest -Uri '${downloadUrl}' -OutFile ${tmpDir}\\${binName}; & '${tmpDir}\\${binName}'"`;
    const cmdCmd = authToken
      ? `curl -sL ${authHeader} "${downloadUrl}" -o "%TEMP%\\${binName}" && "%TEMP%\\${binName}"`
      : `curl -sL "${downloadUrl}" -o "%TEMP%\\${binName}" && "%TEMP%\\${binName}"`;
    return { powershell: psCmd, cmd: cmdCmd };
  }, [platform, arch, serverUrl, psk, authToken]);

  useEffect(() => {
    fetchDevices();
    const interval = setInterval(fetchDevices, 10000);
    return () => clearInterval(interval);
  }, [fetchDevices]);

  const updateNotes = async (deviceId: string, notes: string) => {
    try {
      await apiClient.put(`/devices/${deviceId}/notes`, { notes });
    } catch {
      message.error('更新备注失败');
    }
  };

  const handleDownload = async () => {
    try {
      const values = await form.validateFields();
      setDownloading(true);
      await buildAndDownloadAgent(values.platform, values.arch, values.server_url, values.psk);
      setDownloadOpen(false);
    } catch {
      // error handled in buildAndDownloadAgent
    } finally {
      setDownloading(false);
    }
  };

  const copyToClipboard = (text: string) => {
    navigator.clipboard.writeText(text).then(() => {
      message.success('已复制到剪贴板');
    }).catch(() => {
      message.error('复制失败');
    });
  };

  // Render a copyable command block
  const CommandBlock = ({ command, label }: { command: string; label: string }) => (
    <div style={{ marginBottom: 8 }}>
      <div style={{ fontSize: 12, color: themeToken.colorTextSecondary, marginBottom: 4, display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
        <span>{label}</span>
        <Tooltip title="复制">
          <Button type="text" size="small" icon={<CopyOutlined />} onClick={() => copyToClipboard(command)} />
        </Tooltip>
      </div>
      <pre style={{
        background: themeToken.colorBgContainer,
        border: `1px solid ${themeToken.colorBorderSecondary}`,
        borderRadius: 6,
        padding: '8px 12px',
        fontSize: 12,
        fontFamily: 'monospace',
        whiteSpace: 'pre-wrap',
        wordBreak: 'break-all',
        margin: 0,
        maxHeight: 120,
        overflow: 'auto',
      }}>
        {command}
      </pre>
    </div>
  );

  return (
    <div>
      <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: 16 }}>
        <Title level={4} style={{ margin: 0 }}>设备</Title>
        <div style={{ display: 'flex', gap: 8 }}>
          <Button icon={<DownloadOutlined />} onClick={() => {
            form.setFieldsValue({
              platform: 'windows',
              arch: 'amd64',
              server_url: window.location.host,
              psk: '',
            });
            setDownloadOpen(true);
          }}>下载 Agent</Button>
          <Search placeholder="搜索设备" allowClear style={{ width: 240 }} />
        </div>
      </div>

      <Modal
        title="部署 Agent"
        open={downloadOpen}
        onCancel={() => setDownloadOpen(false)}
        footer={null}
        width={600}
      >
        <Form form={form} layout="vertical" style={{ marginTop: 8 }}>
          <Form.Item name="server_url" label="服务器地址" rules={[{ required: true, message: '请输入服务器地址' }]}>
            <Input placeholder="例如: 192.168.1.100:8443" />
          </Form.Item>
          <Form.Item name="psk" label="PSK (预共享密钥)" extra="留空则使用服务器默认 PSK">
            <Input.Password placeholder="可选，留空使用默认" />
          </Form.Item>
          <Row gutter={16}>
            <Col span={12}>
              <Form.Item name="platform" label="平台" rules={[{ required: true }]}>
                <Select options={[
                  { value: 'windows', label: 'Windows' },
                  { value: 'linux', label: 'Linux' },
                  { value: 'darwin', label: 'macOS' },
                ]} />
              </Form.Item>
            </Col>
            <Col span={12}>
              <Form.Item name="arch" label="架构" rules={[{ required: true }]}>
                <Select options={[
                  { value: 'amd64', label: 'amd64 (x86_64)' },
                  { value: 'arm64', label: 'arm64 (AArch64)' },
                ]} />
              </Form.Item>
            </Col>
          </Row>
        </Form>

        <Tabs
          items={[
            {
              key: 'quick',
              label: '快速安装',
              children: (
                <div>
                  <div style={{ marginBottom: 8, fontSize: 12, color: themeToken.colorTextTertiary }}>
                    在目标设备上执行以下命令，一键下载并运行 Agent：
                  </div>
                  {(platform === 'linux' || platform === 'darwin') ? (
                    <>
                      <CommandBlock command={(installCommands as any).curl} label="curl (推荐)" />
                      <CommandBlock command={(installCommands as any).wget} label="wget" />
                    </>
                  ) : (
                    <>
                      <CommandBlock command={(installCommands as any).powershell} label="PowerShell (推荐)" />
                      <CommandBlock command={(installCommands as any).cmd} label="CMD (curl)" />
                    </>
                  )}
                  <div style={{ marginTop: 8, padding: '6px 10px', background: themeToken.colorWarningBg, borderRadius: 4, fontSize: 11, color: themeToken.colorTextSecondary }}>
                    命令中包含认证令牌，请勿泄露给未授权人员
                  </div>
                </div>
              ),
            },
            {
              key: 'download',
              label: '下载文件',
              children: (
                <div style={{ textAlign: 'center', padding: '24px 0' }}>
                  <Button type="primary" size="large" icon={<DownloadOutlined />} loading={downloading} onClick={handleDownload}>
                    {downloading ? '构建中...' : '构建并下载 Agent'}
                  </Button>
                  <div style={{ marginTop: 8, fontSize: 12, color: themeToken.colorTextTertiary }}>
                    下载 Agent 可执行文件后手动部署到目标设备
                  </div>
                </div>
              ),
            },
          ]}
        />
      </Modal>

      <Row gutter={[16, 16]}>
        {devices.map((device) => (
          <Col xs={24} sm={12} md={8} lg={6} key={device.id}>
            <Card
              hoverable
              loading={loading}
              onClick={() => navigate(`/devices/${device.id}`)}
              actions={[
                <span key="status">
                  <Tag color={device.status === 'online' ? 'green' : 'red'}>
                    {device.status === 'online' ? '在线' : '离线'}
                  </Tag>
                </span>,
              ]}
            >
              <Card.Meta
                avatar={<OsIcon os={device.os} />}
                title={device.hostname || device.id.slice(0, 12)}
                description={
                  <div>
                    <div style={{ fontSize: 12, color: themeToken.colorTextTertiary }}>{device.os} / {device.arch}</div>
                    <div style={{ fontSize: 12, color: themeToken.colorTextTertiary, fontFamily: 'monospace', display: 'flex', alignItems: 'center', gap: 4, flexWrap: 'wrap' }}>
                      <span>IP: {device.ip || 'N/A'}</span>
                      {device.ip_location && <Tag style={{ fontSize: 10, lineHeight: '16px', padding: '0 4px', margin: 0 }}>{device.ip_location}</Tag>}
                    </div>
                    {device.ip_internal && <div style={{ fontSize: 11, color: themeToken.colorTextQuaternary, fontFamily: 'monospace' }}>内网: {device.ip_internal}</div>}
                    <div style={{ fontSize: 12, color: themeToken.colorTextTertiary }}>Agent: {device.agent_version || 'N/A'}</div>
                    <div
                      style={{
                        fontSize: 12,
                        color: device.notes ? themeToken.colorTextSecondary : themeToken.colorTextTertiary,
                        marginTop: 4,
                        borderTop: `1px dashed ${themeToken.colorBorderSecondary}`,
                        paddingTop: 4,
                        cursor: 'text',
                        display: 'flex',
                        alignItems: 'center',
                        gap: 4,
                      }}
                      onClick={(e) => e.stopPropagation()}
                      onMouseDown={(e) => e.stopPropagation()}
                    >
                      <EditOutlined style={{ fontSize: 10 }} />
                      <Input.TextArea
                        value={editingNotes[device.id] !== undefined ? editingNotes[device.id] : (device.notes || '')}
                        placeholder="点击添加备注..."
                        autoSize={{ minRows: 1, maxRows: 3 }}
                        style={{ fontSize: 12, padding: '0 4px', border: 'none', background: 'transparent', resize: 'none' }}
                        onClick={(e) => e.stopPropagation()}
                        onMouseDown={(e) => e.stopPropagation()}
                        onChange={(e) => setEditingNotes(prev => ({ ...prev, [device.id]: e.target.value }))}
                        onBlur={() => {
                          const val = editingNotes[device.id] !== undefined ? editingNotes[device.id] : (device.notes || '');
                          if (val !== (device.notes || '')) {
                            updateNotes(device.id, val);
                          }
                          setEditingNotes(prev => {
                            const next = { ...prev };
                            delete next[device.id];
                            return next;
                          });
                        }}
                        onKeyDown={(e) => e.stopPropagation()}
                      />
                    </div>
                  </div>
                }
              />
            </Card>
          </Col>
        ))}
        {devices.length === 0 && !loading && (
          <Col span={24} style={{ textAlign: 'center', padding: 48, color: themeToken.colorTextTertiary }}>
            暂无设备，请确保 Agent 已正确配置并连接到服务器
          </Col>
        )}
      </Row>
    </div>
  );
}
