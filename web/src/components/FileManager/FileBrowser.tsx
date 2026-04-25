import { useState, useEffect, useRef } from 'react';
import { Table, Breadcrumb, Button, Space, Upload, Modal, message, Typography, Input, Popconfirm, Dropdown } from 'antd';
import { FolderOutlined, FileOutlined, UploadOutlined, DeleteOutlined, EditOutlined, ReloadOutlined, HomeOutlined, SearchOutlined, LockOutlined, FolderAddOutlined, DownloadOutlined, FileAddOutlined } from '@ant-design/icons';
import apiClient from '../../api/client';
import type { FileEntry, FileBrowseResult } from '../../api/types';
import CodeEditor from './CodeEditor';
import { formatBytes, GenerateID } from '../../api/helpers';
import useUploadStore from '../../store/uploadSlice';

const { Text } = Typography;

interface Props {
  deviceId: string;
}

export default function FileBrowser({ deviceId }: Props) {
  const [currentPath, setCurrentPath] = useState('/');
  const [entries, setEntries] = useState<FileEntry[]>([]);
  const [loading, setLoading] = useState(false);
  const [editingFile, setEditingFile] = useState<{ path: string; content: string; language: string } | null>(null);
  const [searchVisible, setSearchVisible] = useState(false);
  const [searchResults, setSearchResults] = useState<FileEntry[]>([]);
  const [searching, setSearching] = useState(false);
  const [chmodVisible, setChmodVisible] = useState(false);
  const [chmodPath, setChmodPath] = useState('');
  const [chmodMode, setChmodMode] = useState('');
  const [mkdirVisible, setMkdirVisible] = useState(false);
  const [createFileVisible, setCreateFileVisible] = useState(false);
  const [isDragOver, setIsDragOver] = useState(false);
  const folderInputRef = useRef<HTMLInputElement>(null);
  const { addTask, updateTask, removeTask } = useUploadStore();

  const fetchFiles = async (path: string) => {
    setLoading(true);
    try {
      const { data } = await apiClient.get<FileBrowseResult>(`/devices/${deviceId}/files`, { params: { path } });
      setEntries(data.entries || []);
      setCurrentPath(data.path);
    } catch {
      message.error('浏览文件失败');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchFiles('/');
  }, [deviceId]);

  const handleNavigate = (path: string) => {
    fetchFiles(path);
  };

  const handleReadFile = async (path: string) => {
    try {
      const { data } = await apiClient.get(`/devices/${deviceId}/files/content`, { params: { path } });
      setEditingFile({ path: data.path, content: data.content, language: data.language });
    } catch (e: any) {
      message.error(e.response?.data?.error || '读取文件失败');
    }
  };

  const handleSaveFile = async (content: string) => {
    if (!editingFile) return;
    try {
      await apiClient.put(`/devices/${deviceId}/files/content`, { path: editingFile.path, content });
      message.success('保存成功');
      setEditingFile(null);
    } catch {
      message.error('保存失败');
    }
  };

  const handleDelete = async (path: string) => {
    try {
      await apiClient.delete(`/devices/${deviceId}/files`, { params: { path } });
      message.success('删除成功');
      fetchFiles(currentPath);
    } catch {
      message.error('删除失败');
    }
  };

  const handleMkdir = async (name: string) => {
    try {
      const path = buildPath(currentPath, name);
      await apiClient.post(`/devices/${deviceId}/files/mkdir`, { path });
      message.success('目录创建成功');
      setMkdirVisible(false);
      fetchFiles(currentPath);
    } catch {
      message.error('创建目录失败');
    }
  };

  const handleCreateFile = async (name: string) => {
    try {
      const path = buildPath(currentPath, name);
      await apiClient.post(`/devices/${deviceId}/files/create`, { path });
      message.success('文件创建成功');
      setCreateFileVisible(false);
      fetchFiles(currentPath);
    } catch {
      message.error('创建文件失败');
    }
  };

  const handleSearch = async (pattern: string) => {
    if (!pattern.trim()) return;
    setSearching(true);
    try {
      const { data } = await apiClient.get(`/devices/${deviceId}/files/search`, {
        params: { path: currentPath, pattern },
      });
      setSearchResults(data.entries || []);
    } catch {
      message.error('搜索失败');
    } finally {
      setSearching(false);
    }
  };

  const handleChmod = async (path: string, mode: string) => {
    try {
      await apiClient.post(`/devices/${deviceId}/files/chmod`, { path, mode });
      message.success('权限修改成功');
      setChmodVisible(false);
      fetchFiles(currentPath);
    } catch (e: any) {
      message.error(e.response?.data?.error || '修改权限失败');
    }
  };

  const handleUpload = async (file: File) => {
    const transferId = GenerateID();
    const targetPath = buildPath(currentPath, file.name);

    // Add task to global upload store
    addTask({
      id: transferId,
      name: file.name,
      deviceId: deviceId,
      percent: 0,
      status: 'uploading',
      startTime: Date.now(),
    });

    try {
      const chunkSize = 256 * 1024;
      const totalChunks = Math.ceil(file.size / chunkSize);

      // Start upload
      await apiClient.post(`/devices/${deviceId}/files/upload`, {
        transfer_id: transferId,
        path: targetPath,
        size: file.size,
      });

      // Send chunks
      for (let i = 0; i < totalChunks; i++) {
        const start = i * chunkSize;
        const end = Math.min(start + chunkSize, file.size);
        const chunk = file.slice(start, end);
        const base64 = await new Promise<string>((resolve, reject) => {
          const reader = new FileReader();
          reader.onload = () => resolve((reader.result as string).split(',')[1]);
          reader.onerror = () => reject(new Error('FileReader failed'));
          reader.readAsDataURL(chunk);
        });

        await apiClient.post(`/devices/${deviceId}/files/upload/chunk`, {
          transfer_id: transferId,
          index: i,
          data: base64,
        });

        updateTask(transferId, { percent: Math.round(((i + 1) / totalChunks) * 100) });
      }

      // Complete upload
      await apiClient.post(`/devices/${deviceId}/files/upload/done`, {
        transfer_id: transferId,
      });

      updateTask(transferId, { status: 'completed', percent: 100 });
      message.success(`${file.name} 上传成功`);
      fetchFiles(currentPath);
    } catch (e: any) {
      updateTask(transferId, { status: 'error', error: e.response?.data?.error || '上传失败' });
      message.error(e.response?.data?.error || '上传失败');
    }
    return false;
  };

  // File download - use HTTP streaming from server
  const handleDownload = (path: string, name: string) => {
    // Use a direct link so the browser handles the download natively
    const token = localStorage.getItem('token') || '';
    const url = `/api/v1/devices/${deviceId}/files/download?path=${encodeURIComponent(path)}&token=${encodeURIComponent(token)}`;
    const link = document.createElement('a');
    link.href = url;
    link.download = name;
    document.body.appendChild(link);
    link.click();
    document.body.removeChild(link);
  };

  // Directory download as ZIP
  const handleDownloadDir = (path: string) => {
    const token = localStorage.getItem('token') || '';
    const url = `/api/v1/devices/${deviceId}/files/download/dir?path=${encodeURIComponent(path)}&token=${encodeURIComponent(token)}`;
    const link = document.createElement('a');
    link.href = url;
    document.body.appendChild(link);
    link.click();
    document.body.removeChild(link);
    message.info('文件夹下载已开始，请等待打包完成');
  };

  // Folder upload - iterate files preserving relative paths
  const handleFolderUpload = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const files = e.target.files;
    if (!files || files.length === 0) return;

    for (let i = 0; i < files.length; i++) {
      const file = files[i];
      const relativePath = file.webkitRelativePath || file.name;
      const targetPath = buildPath(currentPath, relativePath);
      const transferId = GenerateID();

      // Add task to global upload store
      addTask({
        id: transferId,
        name: relativePath,
        deviceId: deviceId,
        percent: 0,
        status: 'uploading',
        startTime: Date.now(),
      });

      try {
        const chunkSize = 256 * 1024;
        const totalChunks = Math.ceil(file.size / chunkSize);

        // Ensure parent directory exists
        const parentDir = targetPath.substring(0, targetPath.lastIndexOf('/'));
        if (parentDir) {
          try {
            await apiClient.post(`/devices/${deviceId}/files/mkdir`, { path: parentDir });
          } catch { /* directory may already exist */ }
        }

        // Start upload
        await apiClient.post(`/devices/${deviceId}/files/upload`, {
          transfer_id: transferId,
          path: targetPath,
          size: file.size,
        });

        // Send chunks
        for (let j = 0; j < totalChunks; j++) {
          const start = j * chunkSize;
          const end = Math.min(start + chunkSize, file.size);
          const chunk = file.slice(start, end);
          const base64 = await new Promise<string>((resolve, reject) => {
            const reader = new FileReader();
            reader.onload = () => resolve((reader.result as string).split(',')[1]);
            reader.onerror = () => reject(new Error('FileReader failed'));
            reader.readAsDataURL(chunk);
          });

          await apiClient.post(`/devices/${deviceId}/files/upload/chunk`, {
            transfer_id: transferId,
            index: j,
            data: base64,
          });

          updateTask(transferId, { percent: Math.round(((j + 1) / totalChunks) * 100) });
        }

        // Complete upload
        await apiClient.post(`/devices/${deviceId}/files/upload/done`, {
          transfer_id: transferId,
        });

        updateTask(transferId, { status: 'completed', percent: 100 });
      } catch (e: any) {
        updateTask(transferId, { status: 'error', error: `上传失败: ${relativePath}` });
        message.error(`上传失败: ${relativePath}`);
      }
    }

    message.success('文件夹上传完成');
    fetchFiles(currentPath);
    e.target.value = '';
  };

  const handleDrop = (e: React.DragEvent) => {
    e.preventDefault();
    e.stopPropagation();
    setIsDragOver(false);
    const files = Array.from(e.dataTransfer.files);
    files.forEach(file => handleUpload(file));
  };

  const buildPath = (basePath: string, name: string): string => {
    if (basePath === '/' && name.match(/^[A-Z]:$/)) {
      return name + '/';
    }
    if (basePath === '/') {
      return '/' + name;
    }
    return basePath + '/' + name;
  };

  const pathParts = currentPath.split('/').filter(Boolean);

  const columns = [
    {
      title: '名称',
      dataIndex: 'name',
      key: 'name',
      render: (name: string, record: FileEntry) => (
        <Space>
          {record.type === 'dir' ? <FolderOutlined style={{ color: '#faad14' }} /> : <FileOutlined />}
          <a
            onClick={() => {
              if (record.type === 'dir') {
                handleNavigate(buildPath(currentPath, name));
              }
            }}
            style={{ cursor: record.type === 'dir' ? 'pointer' : 'default' }}
          >
            {name}
          </a>
        </Space>
      ),
    },
    { title: '大小', dataIndex: 'size', key: 'size', width: 100, render: (v: number, r: FileEntry) => r.type === 'dir' ? '-' : formatBytes(v) },
    { title: '权限', dataIndex: 'mode', key: 'mode', width: 120, render: (v: string, record: FileEntry) => (
      <a onClick={() => { setChmodPath(buildPath(currentPath, record.name)); setChmodMode(v.replace(/[^0-7]/g, '').slice(-3) || '755'); setChmodVisible(true); }} style={{ fontSize: 12 }}>
        {v}
      </a>
    )},
    { title: '修改时间', dataIndex: 'mod_time', key: 'mod_time', width: 170, render: (v: string) => v ? new Date(v).toLocaleString('zh-CN') : '-' },
    {
      title: '操作', key: 'actions', width: 240,
      render: (_: any, record: FileEntry) => (
        <Space size={4}>
          {record.type === 'file' && (
            <Button size="small" icon={<EditOutlined />} onClick={() => handleReadFile(buildPath(currentPath, record.name))}>编辑</Button>
          )}
          <Button size="small" icon={<DownloadOutlined />} onClick={() => {
            const fullPath = buildPath(currentPath, record.name);
            if (record.type === 'dir') {
              handleDownloadDir(fullPath);
            } else {
              handleDownload(fullPath, record.name);
            }
          }}>
            {record.type === 'dir' ? 'ZIP' : '下载'}
          </Button>
          <Button size="small" icon={<LockOutlined />} onClick={() => { setChmodPath(buildPath(currentPath, record.name)); setChmodMode(record.mode.replace(/[^0-7]/g, '').slice(-3) || '755'); setChmodVisible(true); }} />
          <Popconfirm title="确定删除?" onConfirm={() => handleDelete(buildPath(currentPath, record.name))}>
            <Button size="small" danger icon={<DeleteOutlined />} />
          </Popconfirm>
        </Space>
      ),
    },
  ];

  // New create menu items
  const createMenuItems = [
    { key: 'file', label: '新建文件', icon: <FileAddOutlined /> },
    { key: 'dir', label: '新建文件夹', icon: <FolderAddOutlined /> },
  ];

  return (
    <div
      onDragOver={(e) => { e.preventDefault(); setIsDragOver(true); }}
      onDragLeave={() => setIsDragOver(false)}
      onDrop={handleDrop}
      style={{ position: 'relative' }}
    >
      {/* Drag overlay */}
      {isDragOver && (
        <div style={{
          position: 'absolute', inset: 0, zIndex: 100,
          background: 'rgba(82, 196, 26, 0.1)', border: '2px dashed #52c41a',
          borderRadius: 8, display: 'flex', alignItems: 'center', justifyContent: 'center',
        }}>
          <Text style={{ fontSize: 18, color: '#52c41a' }}>释放文件以上传到当前目录</Text>
        </div>
      )}

      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 16 }}>
        <Breadcrumb
          items={[
            { title: <a onClick={() => handleNavigate('/')}><HomeOutlined /></a> },
            ...pathParts.map((part, idx) => {
              const navPath = currentPath.match(/^[A-Z]:\//) && idx === 0
                ? part + '/'
                : '/' + pathParts.slice(0, idx + 1).join('/');
              return { title: <a onClick={() => handleNavigate(navPath)}>{part}</a> };
            }),
          ]}
        />
        <Space>
          <Dropdown
            menu={{ items: createMenuItems, onClick: ({ key }) => { if (key === 'dir') setMkdirVisible(true); else setCreateFileVisible(true); } }}
          >
            <Button icon={<FolderAddOutlined />}>新建</Button>
          </Dropdown>
          <Upload beforeUpload={(file) => { handleUpload(file); return false; }} showUploadList={false}>
            <Button icon={<UploadOutlined />}>上传文件</Button>
          </Upload>
          <Button icon={<UploadOutlined />} onClick={() => folderInputRef.current?.click()}>上传文件夹</Button>
          <Button icon={<SearchOutlined />} onClick={() => setSearchVisible(true)}>搜索</Button>
          <Button icon={<ReloadOutlined />} onClick={() => fetchFiles(currentPath)}>刷新</Button>
        </Space>
      </div>

      {/* Hidden folder input */}
      <input
        ref={folderInputRef}
        type="file"
        style={{ display: 'none' }}
        multiple
        {...{ webkitdirectory: 'true', directory: 'true' } as any}
        onChange={handleFolderUpload}
      />

      <Table
        columns={columns}
        dataSource={entries}
        rowKey="name"
        size="small"
        loading={loading}
        pagination={false}
        onRow={(record) => ({
          onDoubleClick: () => {
            if (record.type === 'dir') {
              handleNavigate(buildPath(currentPath, record.name));
            }
          },
        })}
      />

      {/* Code editor modal */}
      <Modal
        title={`编辑: ${editingFile?.path || ''}`}
        open={!!editingFile}
        onCancel={() => setEditingFile(null)}
        width={800}
        footer={[
          <Button key="cancel" onClick={() => setEditingFile(null)}>取消</Button>,
          <Button key="save" type="primary" onClick={() => editingFile && handleSaveFile(editingFile.content)}>保存</Button>,
        ]}
      >
        {editingFile && (
          <CodeEditor
            value={editingFile.content}
            language={editingFile.language}
            onChange={(val) => setEditingFile({ ...editingFile, content: val })}
          />
        )}
      </Modal>

      {/* Search modal */}
      <Modal
        title={<><SearchOutlined style={{ marginRight: 8 }} />文件搜索</>}
        open={searchVisible}
        onCancel={() => { setSearchVisible(false); setSearchResults([]); }}
        footer={null}
        width={600}
      >
        <Input.Search
          placeholder="输入文件名关键词"
          enterButton="搜索"
          size="large"
          loading={searching}
          onSearch={handleSearch}
          style={{ marginBottom: 16 }}
        />
        <Text type="secondary" style={{ fontSize: 12, display: 'block', marginBottom: 12 }}>
          搜索路径: {currentPath}
        </Text>
        {searchResults.length > 0 && (
          <Table
            columns={[
              { title: '名称', dataIndex: 'name', key: 'name', render: (name: string, r: FileEntry) => (
                <Space>
                  {r.type === 'dir' ? <FolderOutlined style={{ color: '#faad14' }} /> : <FileOutlined />}
                  <a onClick={() => { setSearchVisible(false); setSearchResults([]); if (r.type === 'dir') handleNavigate(buildPath(currentPath, name)); }}>{name}</a>
                </Space>
              )},
              { title: '大小', dataIndex: 'size', key: 'size', width: 80, render: (v: number, r: FileEntry) => r.type === 'dir' ? '-' : formatBytes(v) },
              { title: '权限', dataIndex: 'mode', key: 'mode', width: 100 },
            ]}
            dataSource={searchResults}
            rowKey="name"
            size="small"
            pagination={false}
          />
        )}
      </Modal>

      {/* Chmod modal */}
      <Modal
        title={<><LockOutlined style={{ marginRight: 8 }} />修改权限</>}
        open={chmodVisible}
        onCancel={() => setChmodVisible(false)}
        onOk={() => handleChmod(chmodPath, chmodMode)}
        width={400}
      >
        <div style={{ marginBottom: 12 }}>
          <Text type="secondary" style={{ fontSize: 12 }}>文件: </Text>
          <Text code style={{ fontSize: 12 }}>{chmodPath}</Text>
        </div>
        <Input
          prefix={<LockOutlined />}
          placeholder="例如: 755, 644"
          value={chmodMode}
          onChange={(e) => setChmodMode(e.target.value)}
          maxLength={4}
        />
        <div style={{ marginTop: 12 }}>
          <Text type="secondary" style={{ fontSize: 12 }}>
            常用权限: 755 (可执行), 644 (只读), 600 (私有), 777 (完全开放)
          </Text>
        </div>
      </Modal>

      {/* Mkdir modal */}
      <Modal
        title="新建目录"
        open={mkdirVisible}
        onCancel={() => setMkdirVisible(false)}
        onOk={() => {
          const input = document.querySelector('#mkdir-input') as HTMLInputElement;
          if (input?.value) handleMkdir(input.value);
        }}
        width={400}
      >
        <Input id="mkdir-input" placeholder="目录名称" onPressEnter={(e) => { handleMkdir((e.target as HTMLInputElement).value); }} />
      </Modal>

      {/* Create file modal */}
      <Modal
        title="新建文件"
        open={createFileVisible}
        onCancel={() => setCreateFileVisible(false)}
        onOk={() => {
          const input = document.querySelector('#createfile-input') as HTMLInputElement;
          if (input?.value) handleCreateFile(input.value);
        }}
        width={400}
      >
        <Input id="createfile-input" placeholder="文件名称" onPressEnter={(e) => { handleCreateFile((e.target as HTMLInputElement).value); }} />
      </Modal>
    </div>
  );
}
