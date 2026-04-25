import apiClient from './client';
import { message } from 'antd';

export async function buildAndDownloadAgent(
  platform: string,
  arch: string,
  serverUrl: string,
  psk: string,
) {
  try {
    const { data } = await apiClient.post('/agent/build', {
      platform,
      arch,
      server_url: serverUrl,
      psk,
    }, {
      responseType: 'blob',
      timeout: 120000, // 2 min timeout for build
    });

    // Create download link
    const ext = platform === 'windows' ? '.exe' : '';
    const filename = `agent-${platform}-${arch}${ext}`;
    const url = window.URL.createObjectURL(new Blob([data]));
    const link = document.createElement('a');
    link.href = url;
    link.setAttribute('download', filename);
    document.body.appendChild(link);
    link.click();
    link.remove();
    window.URL.revokeObjectURL(url);

    message.success('Agent 下载成功');
  } catch (e: any) {
    if (e.response?.data instanceof Blob) {
      // Try to read error from blob response
      const text = await e.response.data.text();
      try {
        const err = JSON.parse(text);
        message.error(err.error || err.detail || '构建失败');
      } catch {
        message.error('构建失败');
      }
    } else {
      message.error(e.response?.data?.error || '构建失败');
    }
    throw e;
  }
}
