import { useEffect, useRef, useCallback, useState } from 'react';
import { message, Modal, Input } from 'antd';
import apiClient from '../../api/client';
import type { FileEntry } from '../../api/types';

interface TreeNode {
  name: string;
  path: string;
  type: 'dir' | 'file';
  expanded: boolean;
  loaded: boolean;
  children: TreeNode[];
}

interface Props {
  deviceId: string;
  onOpenFile: (path: string, name: string) => void;
  selectedPath: string | null;
}

function getFileIcon(name: string, type: string): string {
  if (type === 'dir') return '\uD83D\uDCC1'; // 📁
  const ext = name.split('.').pop()?.toLowerCase() || '';
  if (ext === 'sh') return '\u2699'; // ⚙
  if (ext === 'json') return '{ }';
  return '\uD83D\uDCC4'; // 📄
}

function getIconColor(name: string, type: string): string {
  if (type === 'dir') return '#e8a735';
  const ext = name.split('.').pop()?.toLowerCase() || '';
  if (ext === 'sh') return '#0dbc79';
  if (ext === 'json') return '#e5a34b';
  return '#a9a9a9';
}

export default function FileTreePanel({ deviceId, onOpenFile, selectedPath }: Props) {
  const containerRef = useRef<HTMLDivElement>(null);
  const treeRef = useRef<TreeNode[]>([]);
  const selectedRef = useRef<string | null>(null);
  const uploadInputRef = useRef<HTMLInputElement>(null);
  const folderInputRef = useRef<HTMLInputElement>(null);
  const uploadTargetDirRef = useRef<string>('/');
  const [contextMenu, setContextMenu] = useState<{ x: number; y: number; node: TreeNode } | null>(null);
  const [mkdirVisible, setMkdirVisible] = useState(false);
  const [mkdirPath, setMkdirPath] = useState('');
  const [mkdirName, setMkdirName] = useState('');
  const [createFileVisible, setCreateFileVisible] = useState(false);
  const [createFilePath, setCreateFilePath] = useState('');
  const [createFileName, setCreateFileName] = useState('');
  const [chmodVisible, setChmodVisible] = useState(false);
  const [chmodPath, setChmodPath] = useState('');
  const [chmodMode, setChmodMode] = useState('755');
  const [searchVisible, setSearchVisible] = useState(false);
  const [searchPattern, setSearchPattern] = useState('');
  const [searchResults, setSearchResults] = useState<FileEntry[]>([]);

  const fetchDirectory = useCallback(async (path: string): Promise<FileEntry[]> => {
    try {
      const { data } = await apiClient.get(`/devices/${deviceId}/files`, { params: { path } });
      return data.entries || [];
    } catch (e: any) {
      message.error(e.response?.data?.error || '浏览目录失败');
      return [];
    }
  }, [deviceId]);

  const buildNode = (entry: FileEntry, parentPath: string): TreeNode => ({
    name: entry.name,
    path: parentPath === '/' ? `/${entry.name}` : `${parentPath}/${entry.name}`,
    type: entry.type as 'dir' | 'file',
    expanded: false,
    loaded: false,
    children: [],
  });

  // Close context menu on click elsewhere
  useEffect(() => {
    const handler = () => setContextMenu(null);
    document.addEventListener('click', handler);
    return () => document.removeEventListener('click', handler);
  }, []);

  // Pure JS rendering
  const renderTree = useCallback(() => {
    const container = containerRef.current;
    if (!container) return;
    container.innerHTML = '';

    const renderNode = (node: TreeNode, depth: number, parent: HTMLElement) => {
      const row = document.createElement('div');
      row.style.cssText = `display:flex;align-items:center;padding:2px 8px 2px ${depth * 16 + 8}px;cursor:pointer;white-space:nowrap;font-size:13px;color:#cccccc;user-select:none;`;
      if (selectedRef.current === node.path) {
        row.style.backgroundColor = '#094771';
      }
      row.addEventListener('mouseenter', () => { if (selectedRef.current !== node.path) row.style.backgroundColor = '#2a2d2e'; });
      row.addEventListener('mouseleave', () => { if (selectedRef.current !== node.path) row.style.backgroundColor = ''; });

      // Arrow for directories
      if (node.type === 'dir') {
        const arrow = document.createElement('span');
        arrow.textContent = node.expanded ? '\u25BC' : '\u25B6';
        arrow.style.cssText = 'width:14px;font-size:10px;color:#cccccc;margin-right:2px;flex-shrink:0;';
        row.appendChild(arrow);
      } else {
        const spacer = document.createElement('span');
        spacer.style.cssText = 'width:16px;flex-shrink:0;';
        row.appendChild(spacer);
      }

      // Icon
      const icon = document.createElement('span');
      icon.textContent = getFileIcon(node.name, node.type);
      icon.style.cssText = `margin-right:6px;font-size:13px;color:${getIconColor(node.name, node.type)};flex-shrink:0;`;
      row.appendChild(icon);

      // Name
      const nameSpan = document.createElement('span');
      nameSpan.textContent = node.name;
      nameSpan.style.cssText = 'overflow:hidden;text-overflow:ellipsis;';
      row.appendChild(nameSpan);

      // Click handler
      row.addEventListener('click', async () => {
        if (node.type === 'dir') {
          if (!node.loaded) {
            const entries = await fetchDirectory(node.path);
            node.children = entries
              .filter(e => e.name !== '.' && e.name !== '..')
              .map(e => buildNode(e, node.path));
            node.loaded = true;
          }
          node.expanded = !node.expanded;
          renderTree();
        } else {
          selectedRef.current = node.path;
          onOpenFile(node.path, node.name);
          renderTree();
        }
      });

      // Right-click context menu
      row.addEventListener('contextmenu', (e: MouseEvent) => {
        e.preventDefault();
        e.stopPropagation();
        setContextMenu({ x: e.clientX, y: e.clientY, node });
      });

      parent.appendChild(row);

      // Children
      if (node.type === 'dir' && node.expanded && node.children.length > 0) {
        const childContainer = document.createElement('div');
        for (const child of node.children) {
          renderNode(child, depth + 1, childContainer);
        }
        parent.appendChild(childContainer);
      }
    };

    for (const node of treeRef.current) {
      renderNode(node, 0, container);
    }
  }, [fetchDirectory, onOpenFile]);

  // Update selectedPath ref
  useEffect(() => {
    selectedRef.current = selectedPath;
    renderTree();
  }, [selectedPath, renderTree]);

  // Initial load
  useEffect(() => {
    const loadRoot = async () => {
      const entries = await fetchDirectory('/');
      treeRef.current = entries
        .filter(e => e.name !== '.' && e.name !== '..')
        .map(e => buildNode(e, '/'));
      renderTree();
    };
    loadRoot();
  }, [deviceId, fetchDirectory, renderTree]);

  // Refresh
  const handleRefresh = useCallback(async () => {
    const entries = await fetchDirectory('/');
    treeRef.current = entries
      .filter(e => e.name !== '.' && e.name !== '..')
      .map(e => buildNode(e, '/'));
    const refreshExpanded = async (nodes: TreeNode[]) => {
      for (const node of nodes) {
        if (node.type === 'dir' && node.expanded) {
          const subEntries = await fetchDirectory(node.path);
          node.children = subEntries.filter(e => e.name !== '.' && e.name !== '..').map(e => buildNode(e, node.path));
          node.loaded = true;
          await refreshExpanded(node.children);
        }
      }
    };
    await refreshExpanded(treeRef.current);
    renderTree();
  }, [fetchDirectory, renderTree]);

  // Upload single file
  const handleUpload = useCallback(async (e: React.ChangeEvent<HTMLInputElement>, targetDir: string) => {
    const file = e.target.files?.[0];
    if (!file) return;
    try {
      const filePath = targetDir === '/' ? `/${file.name}` : `${targetDir}/${file.name}`;
      const CHUNK_SIZE = 256 * 1024;
      const transferId = `upload-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;

      const startResp = await apiClient.post(`/devices/${deviceId}/files/upload`, {
        path: filePath,
        size: file.size,
        transfer_id: transferId,
      });

      const actualTransferId = startResp.data?.transfer_id || transferId;
      const serverChunkSize = startResp.data?.chunk_size || CHUNK_SIZE;

      let offset = 0;
      let chunkIndex = 0;
      while (offset < file.size) {
        const chunk = file.slice(offset, offset + serverChunkSize);
        const reader = new FileReader();
        const dataUrl = await new Promise<string>((resolve, reject) => {
          reader.onload = () => resolve(reader.result as string);
          reader.onerror = () => reject(new Error('FileReader failed'));
          reader.readAsDataURL(chunk);
        });
        const base64 = dataUrl.split(',')[1];
        await apiClient.post(`/devices/${deviceId}/files/upload/chunk`, {
          transfer_id: actualTransferId,
          index: chunkIndex,
          data: base64,
        });
        offset += serverChunkSize;
        chunkIndex++;
      }

      await apiClient.post(`/devices/${deviceId}/files/upload/done`, { transfer_id: actualTransferId });
      message.success(`已上传: ${file.name}`);
      handleRefresh();
    } catch (err: any) {
      message.error(err.response?.data?.error || '上传失败');
    }
    e.target.value = '';
  }, [deviceId, handleRefresh]);

  // Folder upload
  const handleFolderUpload = useCallback(async (e: React.ChangeEvent<HTMLInputElement>, targetDir: string) => {
    const files = e.target.files;
    if (!files || files.length === 0) return;

    const CHUNK_SIZE = 256 * 1024;
    for (let i = 0; i < files.length; i++) {
      const file = files[i];
      const relativePath = file.webkitRelativePath || file.name;
      const targetPath = targetDir === '/' ? `/${relativePath}` : `${targetDir}/${relativePath}`;
      const transferId = `upload-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;

      try {
        // Ensure parent directory exists
        const lastSlash = targetPath.lastIndexOf('/');
        if (lastSlash > 0) {
          const parentDir = targetPath.substring(0, lastSlash);
          try {
            await apiClient.post(`/devices/${deviceId}/files/mkdir`, { path: parentDir });
          } catch { /* may already exist */ }
        }

        await apiClient.post(`/devices/${deviceId}/files/upload`, {
          path: targetPath,
          size: file.size,
          transfer_id: transferId,
        });

        const totalChunks = Math.ceil(file.size / CHUNK_SIZE);
        for (let j = 0; j < totalChunks; j++) {
          const start = j * CHUNK_SIZE;
          const end = Math.min(start + CHUNK_SIZE, file.size);
          const chunk = file.slice(start, end);
          const reader = new FileReader();
          const dataUrl = await new Promise<string>((resolve, reject) => {
            reader.onload = () => resolve(reader.result as string);
            reader.onerror = () => reject(new Error('FileReader failed'));
            reader.readAsDataURL(chunk);
          });
          const base64 = dataUrl.split(',')[1];
          await apiClient.post(`/devices/${deviceId}/files/upload/chunk`, {
            transfer_id: transferId,
            index: j,
            data: base64,
          });
        }

        await apiClient.post(`/devices/${deviceId}/files/upload/done`, { transfer_id: transferId });
      } catch (err: any) {
        message.error(`上传失败: ${relativePath}`);
      }
    }

    message.success('文件夹上传完成');
    handleRefresh();
    e.target.value = '';
  }, [deviceId, handleRefresh]);

  // Delete file/directory
  const handleDelete = useCallback(async (path: string) => {
    try {
      await apiClient.delete(`/devices/${deviceId}/files`, { params: { path } });
      message.success(`已删除: ${path}`);
      handleRefresh();
    } catch (err: any) {
      message.error(err.response?.data?.error || '删除失败');
    }
  }, [deviceId, handleRefresh]);

  // Download file via HTTP streaming
  const handleDownload = useCallback((path: string) => {
    const token = localStorage.getItem('token') || '';
    const name = path.split('/').pop() || 'download';
    const url = `/api/v1/devices/${deviceId}/files/download?path=${encodeURIComponent(path)}&token=${encodeURIComponent(token)}`;
    const link = document.createElement('a');
    link.href = url;
    link.download = name;
    document.body.appendChild(link);
    link.click();
    document.body.removeChild(link);
  }, [deviceId]);

  // Download directory as ZIP
  const handleDownloadDir = useCallback((path: string) => {
    const token = localStorage.getItem('token') || '';
    const url = `/api/v1/devices/${deviceId}/files/download/dir?path=${encodeURIComponent(path)}&token=${encodeURIComponent(token)}`;
    const link = document.createElement('a');
    link.href = url;
    document.body.appendChild(link);
    link.click();
    document.body.removeChild(link);
    message.info('文件夹下载已开始，请等待打包完成');
  }, [deviceId]);

  // Create directory
  const handleMkdir = useCallback(async () => {
    if (!mkdirName.trim()) return;
    try {
      const fullPath = mkdirPath === '/' ? `/${mkdirName.trim()}` : `${mkdirPath}/${mkdirName.trim()}`;
      await apiClient.post(`/devices/${deviceId}/files/mkdir`, { path: fullPath });
      message.success(`已创建目录: ${mkdirName}`);
      setMkdirVisible(false);
      setMkdirName('');
      handleRefresh();
    } catch (err: any) {
      message.error(err.response?.data?.error || '创建目录失败');
    }
  }, [deviceId, mkdirPath, mkdirName, handleRefresh]);

  // Create file
  const handleCreateFile = useCallback(async () => {
    if (!createFileName.trim()) return;
    try {
      const fullPath = createFilePath === '/' ? `/${createFileName.trim()}` : `${createFilePath}/${createFileName.trim()}`;
      await apiClient.post(`/devices/${deviceId}/files/create`, { path: fullPath });
      message.success(`已创建文件: ${createFileName}`);
      setCreateFileVisible(false);
      setCreateFileName('');
      handleRefresh();
    } catch (err: any) {
      message.error(err.response?.data?.error || '创建文件失败');
    }
  }, [deviceId, createFilePath, createFileName, handleRefresh]);

  // Chmod
  const handleChmod = useCallback(async () => {
    if (!chmodMode.trim()) return;
    try {
      await apiClient.post(`/devices/${deviceId}/files/chmod`, { path: chmodPath, mode: chmodMode });
      message.success(`权限已修改: ${chmodPath} -> ${chmodMode}`);
      setChmodVisible(false);
    } catch (err: any) {
      message.error(err.response?.data?.error || '修改权限失败');
    }
  }, [deviceId, chmodPath, chmodMode]);

  // Search files
  const handleSearch = useCallback(async () => {
    if (!searchPattern.trim()) return;
    try {
      const { data } = await apiClient.get(`/devices/${deviceId}/files/search`, {
        params: { path: '/', pattern: searchPattern },
      });
      setSearchResults(data.entries || []);
    } catch (err: any) {
      message.error(err.response?.data?.error || '搜索失败');
    }
  }, [deviceId, searchPattern]);

  // Context menu actions
  const contextActions = contextMenu ? [
    ...(contextMenu.node.type === 'dir' ? [
      { label: '新建文件', action: () => { setCreateFilePath(contextMenu.node.path); setCreateFileVisible(true); } },
      { label: '新建文件夹', action: () => { setMkdirPath(contextMenu.node.path); setMkdirVisible(true); } },
      { label: '上传文件', action: () => { uploadTargetDirRef.current = contextMenu.node.path; uploadInputRef.current?.click(); } },
      { label: '上传文件夹', action: () => { uploadTargetDirRef.current = contextMenu.node.path; folderInputRef.current?.click(); } },
      { label: '下载 (ZIP)', action: () => handleDownloadDir(contextMenu.node.path) },
    ] : [
      { label: '下载', action: () => handleDownload(contextMenu.node.path) },
      { label: '修改权限', action: () => { setChmodPath(contextMenu.node.path); setChmodVisible(true); } },
    ]),
    { label: '删除', action: () => {
      Modal.confirm({
        title: `确定删除 ${contextMenu.node.name}?`,
        content: contextMenu.node.type === 'dir' ? '将删除文件夹及其所有内容' : `路径: ${contextMenu.node.path}`,
        okText: '删除',
        okType: 'danger',
        cancelText: '取消',
        onOk: () => handleDelete(contextMenu.node.path),
      });
    }},
  ] : [];

  const toolbarBtnStyle: React.CSSProperties = {
    background: 'none', border: 'none', color: '#cccccc', cursor: 'pointer',
    fontSize: 13, padding: '2px 6px', borderRadius: 3,
  };

  return (
    <div style={{ height: '100%', display: 'flex', flexDirection: 'column', background: '#252526' }}>
      {/* Toolbar */}
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', padding: '6px 8px', borderBottom: '1px solid #3c3c3c', flexShrink: 0 }}>
        <span style={{ color: '#bbbbbb', fontSize: 11, textTransform: 'uppercase', letterSpacing: 1 }}>EXPLORER</span>
        <div style={{ display: 'flex', gap: 2 }}>
          <button onClick={handleRefresh} style={toolbarBtnStyle} title="刷新">&#x21bb;</button>
          <button onClick={() => { setCreateFilePath('/'); setCreateFileVisible(true); }} style={toolbarBtnStyle} title="新建文件">f</button>
          <button onClick={() => { setMkdirPath('/'); setMkdirVisible(true); }} style={toolbarBtnStyle} title="新建文件夹">+</button>
          <button onClick={() => { uploadTargetDirRef.current = '/'; uploadInputRef.current?.click(); }} style={toolbarBtnStyle} title="上传文件">&#x2191;</button>
          <button onClick={() => { uploadTargetDirRef.current = '/'; folderInputRef.current?.click(); }} style={toolbarBtnStyle} title="上传文件夹">&#x2191;&#x2191;</button>
          <button onClick={() => setSearchVisible(true)} style={toolbarBtnStyle} title="搜索文件">&#x26B2;</button>
        </div>
      </div>

      {/* Hidden file upload input */}
      <input
        ref={uploadInputRef}
        type="file"
        style={{ display: 'none' }}
        onChange={(e) => {
          handleUpload(e, uploadTargetDirRef.current);
        }}
      />

      {/* Hidden folder upload input */}
      <input
        ref={folderInputRef}
        type="file"
        style={{ display: 'none' }}
        multiple
        {...{ webkitdirectory: 'true', directory: 'true' } as any}
        onChange={(e) => {
          handleFolderUpload(e, uploadTargetDirRef.current);
        }}
      />

      {/* File tree */}
      <div ref={containerRef} style={{ flex: 1, overflow: 'auto', padding: '4px 0' }} />

      {/* Search results */}
      {searchVisible && searchResults.length > 0 && (
        <div style={{ maxHeight: 150, overflow: 'auto', borderTop: '1px solid #3c3c3c', padding: '4px 0', background: '#1e1e1e' }}>
          {searchResults.map((entry, i) => (
            <div
              key={i}
              onClick={() => {
                if (entry.type === 'file') {
                  const filePath = entry.path || entry.name;
                  onOpenFile(filePath, entry.name);
                }
                setSearchVisible(false);
                setSearchResults([]);
                setSearchPattern('');
              }}
              style={{ padding: '2px 12px', cursor: 'pointer', fontSize: 12, color: '#cccccc' }}
              onMouseEnter={(e) => { e.currentTarget.style.backgroundColor = '#2a2d2e'; }}
              onMouseLeave={(e) => { e.currentTarget.style.backgroundColor = ''; }}
            >
              {getFileIcon(entry.name, entry.type)} {entry.name}
              {entry.path && entry.path !== '/' + entry.name && <span style={{ color: '#666', marginLeft: 8 }}>({entry.path})</span>}
            </div>
          ))}
        </div>
      )}

      {/* Context Menu */}
      {contextMenu && (
        <div
          style={{
            position: 'fixed', left: contextMenu.x, top: contextMenu.y,
            background: '#2d2d2d', border: '1px solid #454545', borderRadius: 4,
            boxShadow: '0 4px 12px rgba(0,0,0,0.4)', zIndex: 9999, minWidth: 140,
            padding: '4px 0',
          }}
          onClick={(e) => e.stopPropagation()}
        >
          {contextActions.map((item, i) => (
            <div
              key={i}
              onClick={() => { item.action(); setContextMenu(null); }}
              style={{
                padding: '6px 16px', cursor: 'pointer', fontSize: 12, color: '#cccccc',
              }}
              onMouseEnter={(e) => { e.currentTarget.style.backgroundColor = '#094771'; }}
              onMouseLeave={(e) => { e.currentTarget.style.backgroundColor = ''; }}
            >
              {item.label}
            </div>
          ))}
        </div>
      )}

      {/* Create File Modal */}
      <Modal
        title="新建文件"
        open={createFileVisible}
        onOk={handleCreateFile}
        onCancel={() => { setCreateFileVisible(false); setCreateFileName(''); }}
        okText="创建"
        cancelText="取消"
        width={400}
      >
        <div style={{ marginBottom: 8 }}>
          <span style={{ color: '#888', fontSize: 12 }}>位置: {createFilePath}</span>
        </div>
        <Input
          placeholder="文件名称"
          value={createFileName}
          onChange={(e) => setCreateFileName(e.target.value)}
          onPressEnter={handleCreateFile}
          autoFocus
        />
      </Modal>

      {/* Mkdir Modal */}
      <Modal
        title="新建文件夹"
        open={mkdirVisible}
        onOk={handleMkdir}
        onCancel={() => { setMkdirVisible(false); setMkdirName(''); }}
        okText="创建"
        cancelText="取消"
        width={400}
      >
        <div style={{ marginBottom: 8 }}>
          <span style={{ color: '#888', fontSize: 12 }}>位置: {mkdirPath}</span>
        </div>
        <Input
          placeholder="文件夹名称"
          value={mkdirName}
          onChange={(e) => setMkdirName(e.target.value)}
          onPressEnter={handleMkdir}
          autoFocus
        />
      </Modal>

      {/* Chmod Modal */}
      <Modal
        title="修改权限"
        open={chmodVisible}
        onOk={handleChmod}
        onCancel={() => { setChmodVisible(false); }}
        okText="修改"
        cancelText="取消"
        width={400}
      >
        <div style={{ marginBottom: 8 }}>
          <span style={{ color: '#888', fontSize: 12 }}>文件: {chmodPath}</span>
        </div>
        <Input
          placeholder="权限 (如 755, 644)"
          value={chmodMode}
          onChange={(e) => setChmodMode(e.target.value)}
          onPressEnter={handleChmod}
          autoFocus
        />
      </Modal>

      {/* Search Modal */}
      <Modal
        title="搜索文件"
        open={searchVisible}
        onOk={handleSearch}
        onCancel={() => { setSearchVisible(false); setSearchPattern(''); setSearchResults([]); }}
        okText="搜索"
        cancelText="取消"
        width={400}
      >
        <Input
          placeholder="文件名模式 (如 *.log, config)"
          value={searchPattern}
          onChange={(e) => setSearchPattern(e.target.value)}
          onPressEnter={handleSearch}
          autoFocus
        />
      </Modal>
    </div>
  );
}
