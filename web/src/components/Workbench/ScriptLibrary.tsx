import { useState } from 'react';
import { Typography, Input, Tag, Collapse } from 'antd';
import { CodeOutlined, SearchOutlined } from '@ant-design/icons';

const { Text } = Typography;

interface ScriptItem {
  name: string;
  command: string;
  tags?: string[];
}

interface ScriptCategory {
  label: string;
  scripts: ScriptItem[];
}

const SCRIPT_LIBRARY: ScriptCategory[] = [
  {
    label: '系统信息',
    scripts: [
      { name: '系统概览', command: 'uname -a && uptime && free -h && df -h', tags: ['linux'] },
      { name: 'CPU信息', command: 'lscpu | head -20', tags: ['linux'] },
      { name: '内存使用', command: 'free -h && echo "---" && ps aux --sort=-%mem | head -10', tags: ['linux'] },
      { name: '磁盘使用', command: 'df -hT && echo "---" && du -sh /* 2>/dev/null | sort -rh | head -10', tags: ['linux'] },
      { name: '网络连接', command: 'ss -tunlp && echo "---" && ip addr show', tags: ['linux'] },
      { name: '系统信息(Windows)', command: 'systeminfo | findstr /C:"OS" /C:"System" /C:"Processor" /C:"Memory"', tags: ['windows'] },
    ],
  },
  {
    label: '进程管理',
    scripts: [
      { name: 'Top 10 CPU进程', command: 'ps aux --sort=-%cpu | head -11', tags: ['linux'] },
      { name: 'Top 10 内存进程', command: 'ps aux --sort=-%mem | head -11', tags: ['linux'] },
      { name: '僵尸进程', command: 'ps aux | grep -w Z', tags: ['linux'] },
      { name: '监听端口', command: 'ss -tlnp', tags: ['linux'] },
      { name: '进程树', command: 'ps auxf | head -50', tags: ['linux'] },
    ],
  },
  {
    label: '网络诊断',
    scripts: [
      { name: '路由表', command: 'ip route show', tags: ['linux'] },
      { name: 'DNS解析测试', command: 'nslookup google.com 2>/dev/null || dig google.com +short', tags: ['linux'] },
      { name: 'TCP连接统计', command: 'ss -s && echo "---" && ss -t state established | wc -l', tags: ['linux'] },
      { name: 'Ping测试', command: 'ping -c 4 8.8.8.8', tags: ['linux'] },
      { name: 'Traceroute', command: 'traceroute -m 20 8.8.8.8 2>/dev/null || tracert -d -h 20 8.8.8.8', tags: ['linux', 'windows'] },
      { name: '防火墙规则', command: 'iptables -L -n --line-numbers 2>/dev/null || netsh advfirewall firewall show rule name=all', tags: ['linux', 'windows'] },
    ],
  },
  {
    label: '文件操作',
    scripts: [
      { name: '大文件查找', command: 'find / -xdev -type f -size +100M 2>/dev/null | head -20', tags: ['linux'] },
      { name: '最近修改文件', command: 'find /etc -type f -mtime -1 2>/dev/null', tags: ['linux'] },
      { name: '目录大小排序', command: 'du -sh /var/log /tmp /home /root 2>/dev/null | sort -rh', tags: ['linux'] },
      { name: '日志查看', command: 'tail -100 /var/log/syslog 2>/dev/null || tail -100 /var/log/messages 2>/dev/null', tags: ['linux'] },
      { name: 'SUID文件', command: 'find / -perm -4000 -type f 2>/dev/null', tags: ['linux'] },
    ],
  },
  {
    label: 'Docker',
    scripts: [
      { name: '容器列表', command: 'docker ps -a', tags: ['docker'] },
      { name: '容器资源', command: 'docker stats --no-stream', tags: ['docker'] },
      { name: '镜像列表', command: 'docker images', tags: ['docker'] },
      { name: '磁盘占用', command: 'docker system df', tags: ['docker'] },
      { name: '容器日志', command: 'docker ps -q | head -1 | xargs -I{} docker logs --tail 50 {}', tags: ['docker'] },
    ],
  },
  {
    label: '服务管理',
    scripts: [
      { name: '服务状态', command: 'systemctl list-units --type=service --state=running | head -20', tags: ['linux'] },
      { name: '失败服务', command: 'systemctl --failed', tags: ['linux'] },
      { name: '服务日志', command: 'journalctl -u sshd --no-pager -n 50 2>/dev/null || journalctl -u ssh --no-pager -n 50', tags: ['linux'] },
      { name: '定时任务', command: 'crontab -l 2>/dev/null && echo "---" && ls -la /etc/cron.d/', tags: ['linux'] },
    ],
  },
];

interface Props {
  onRun: (command: string) => void;
}

export default function ScriptLibrary({ onRun }: Props) {
  const [search, setSearch] = useState('');

  const filteredLibrary = SCRIPT_LIBRARY.map(cat => ({
    ...cat,
    scripts: cat.scripts.filter(s =>
      !search ||
      s.name.toLowerCase().includes(search.toLowerCase()) ||
      s.command.toLowerCase().includes(search.toLowerCase())
    ),
  })).filter(cat => cat.scripts.length > 0);

  return (
    <div style={{ height: '100%', display: 'flex', flexDirection: 'column', background: '#252526' }}>
      <div style={{ padding: '8px 10px', borderBottom: '1px solid #3c3c3c' }}>
        <Input
          prefix={<SearchOutlined />}
          placeholder="搜索脚本..."
          size="small"
          value={search}
          onChange={e => setSearch(e.target.value)}
          allowClear
        />
      </div>
      <div style={{ flex: 1, overflow: 'auto', padding: '4px 0' }}>
        <Collapse
          ghost
          size="small"
          defaultActiveKey={SCRIPT_LIBRARY.map((_, i) => String(i))}
          items={filteredLibrary.map((cat, idx) => ({
            key: String(idx),
            label: <Text style={{ fontSize: 11, color: '#cccccc', fontWeight: 600 }}>{cat.label}</Text>,
            children: cat.scripts.map((script, si) => (
              <div
                key={si}
                onClick={() => onRun(script.command)}
                style={{
                  padding: '4px 8px',
                  cursor: 'pointer',
                  borderRadius: 4,
                  marginBottom: 2,
                  borderLeft: '2px solid transparent',
                }}
                onMouseEnter={e => {
                  e.currentTarget.style.background = '#2a2d2e';
                  e.currentTarget.style.borderLeftColor = '#007acc';
                }}
                onMouseLeave={e => {
                  e.currentTarget.style.background = 'transparent';
                  e.currentTarget.style.borderLeftColor = 'transparent';
                }}
                title={script.command}
              >
                <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                  <Text style={{ fontSize: 12, color: '#cccccc' }}>{script.name}</Text>
                  {script.tags?.map(t => (
                    <Tag key={t} style={{ fontSize: 10, lineHeight: '16px', padding: '0 4px', margin: 0 }}>{t}</Tag>
                  ))}
                </div>
                <Text ellipsis style={{ fontSize: 10, color: '#6a9955', display: 'block', maxWidth: '100%' }}>
                  {script.command}
                </Text>
              </div>
            )),
          }))}
        />
      </div>
    </div>
  );
}
