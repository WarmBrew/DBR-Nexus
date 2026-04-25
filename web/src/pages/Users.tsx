import { useEffect, useState } from 'react';
import { Table, Button, Modal, Form, Input, Select, Switch, Popconfirm, Typography, message, Tag, Space, DatePicker } from 'antd';
import { PlusOutlined, EditOutlined, KeyOutlined, ExportOutlined } from '@ant-design/icons';
import apiClient from '../api/client';
import dayjs from 'dayjs';

const { Title } = Typography;
const { RangePicker } = DatePicker;

interface User {
  id: string;
  username: string;
  display_name: string;
  role: string;
  enabled: boolean;
}

export default function Users() {
  const [users, setUsers] = useState<User[]>([]);
  const [form] = Form.useForm();
  const [editForm] = Form.useForm();
  const [createVisible, setCreateVisible] = useState(false);
  const [editVisible, setEditVisible] = useState(false);
  const [passwordVisible, setPasswordVisible] = useState(false);
  const [editingUser, setEditingUser] = useState<User | null>(null);

  const fetchUsers = async () => {
    try {
      const { data } = await apiClient.get<User[]>('/users');
      setUsers(data || []);
    } catch { /* ignore */ }
  };

  useEffect(() => { fetchUsers(); }, []);

  const handleCreate = async (values: any) => {
    try {
      await apiClient.post('/users', values);
      message.success('用户创建成功');
      setCreateVisible(false);
      form.resetFields();
      fetchUsers();
    } catch (e: any) {
      message.error(e.response?.data?.error || '创建失败');
    }
  };

  const handleDelete = async (id: string) => {
    try {
      await apiClient.delete(`/users/${id}`);
      message.success('用户已删除');
      fetchUsers();
    } catch {
      message.error('删除失败');
    }
  };

  const handleToggleEnabled = async (id: string, enabled: boolean) => {
    try {
      await apiClient.put(`/users/${id}`, { enabled });
      fetchUsers();
    } catch {
      message.error('更新失败');
    }
  };

  const handleEdit = (user: User) => {
    setEditingUser(user);
    editForm.setFieldsValue({ display_name: user.display_name, role: user.role });
    setEditVisible(true);
  };

  const handleEditSubmit = async (values: any) => {
    if (!editingUser) return;
    try {
      await apiClient.put(`/users/${editingUser.id}`, values);
      message.success('用户信息已更新');
      setEditVisible(false);
      fetchUsers();
    } catch (e: any) {
      message.error(e.response?.data?.error || '更新失败');
    }
  };

  const handleResetPassword = async (values: { password: string }) => {
    if (!editingUser) return;
    try {
      await apiClient.put(`/users/${editingUser.id}`, { password: values.password });
      message.success('密码已重置');
      setPasswordVisible(false);
    } catch (e: any) {
      message.error(e.response?.data?.error || '重置失败');
    }
  };

  const columns = [
    { title: '用户名', dataIndex: 'username', key: 'username' },
    { title: '显示名', dataIndex: 'display_name', key: 'display_name' },
    { title: '角色', dataIndex: 'role', key: 'role', render: (v: string) => {
      const colors: Record<string, string> = { admin: 'red', operator: 'blue', viewer: 'green' };
      const labels: Record<string, string> = { admin: '管理员', operator: '操作员', viewer: '查看者' };
      return <Tag color={colors[v] || 'default'}>{labels[v] || v}</Tag>;
    }},
    { title: '状态', dataIndex: 'enabled', key: 'enabled', render: (v: boolean, record: User) => (
      <Switch checked={v} onChange={(checked) => handleToggleEnabled(record.id, checked)} />
    )},
    {
      title: '操作', key: 'action',
      render: (_: any, record: User) => (
        <Space size="small">
          <Button size="small" icon={<EditOutlined />} onClick={() => handleEdit(record)}>编辑</Button>
          <Button size="small" icon={<KeyOutlined />} onClick={() => { setEditingUser(record); setPasswordVisible(true); }}>重置密码</Button>
          <Popconfirm title="确定删除?" onConfirm={() => handleDelete(record.id)}>
            <Button size="small" danger disabled={record.username === 'admin'}>删除</Button>
          </Popconfirm>
        </Space>
      ),
    },
  ];

  return (
    <div>
      <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: 16 }}>
        <Title level={4} style={{ margin: 0 }}>用户管理</Title>
        <Button type="primary" icon={<PlusOutlined />} onClick={() => setCreateVisible(true)}>创建用户</Button>
      </div>

      <Table columns={columns} dataSource={users} rowKey="id" size="small" />

      <Modal title="创建用户" open={createVisible} onCancel={() => setCreateVisible(false)} onOk={() => form.submit()}>
        <Form form={form} onFinish={handleCreate} layout="vertical">
          <Form.Item name="username" label="用户名" rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <Form.Item name="password" label="密码" rules={[{ required: true, min: 6 }]}>
            <Input.Password />
          </Form.Item>
          <Form.Item name="display_name" label="显示名">
            <Input />
          </Form.Item>
          <Form.Item name="role" label="角色" rules={[{ required: true }]} initialValue="operator">
            <Select options={[
              { value: 'admin', label: '管理员' },
              { value: 'operator', label: '操作员' },
              { value: 'viewer', label: '查看者' },
            ]} />
          </Form.Item>
        </Form>
      </Modal>

      <Modal title={`编辑用户 - ${editingUser?.username}`} open={editVisible} onCancel={() => setEditVisible(false)} onOk={() => editForm.submit()}>
        <Form form={editForm} onFinish={handleEditSubmit} layout="vertical">
          <Form.Item name="display_name" label="显示名">
            <Input />
          </Form.Item>
          <Form.Item name="role" label="角色" rules={[{ required: true }]}>
            <Select options={[
              { value: 'admin', label: '管理员' },
              { value: 'operator', label: '操作员' },
              { value: 'viewer', label: '查看者' },
            ]} />
          </Form.Item>
        </Form>
      </Modal>

      <Modal title={`重置密码 - ${editingUser?.username}`} open={passwordVisible} onCancel={() => setPasswordVisible(false)} onOk={() => {
        const form = document.querySelector('#password-form') as any;
      }} footer={null}>
        <Form onFinish={handleResetPassword} layout="vertical" id="password-form">
          <Form.Item name="password" label="新密码" rules={[{ required: true, min: 6, message: '密码至少6位' }]}>
            <Input.Password />
          </Form.Item>
          <Form.Item>
            <Button type="primary" htmlType="submit">确认重置</Button>
          </Form.Item>
        </Form>
      </Modal>
    </div>
  );
}
