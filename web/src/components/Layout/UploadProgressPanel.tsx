import { useMemo, useEffect, useCallback } from 'react';
import { Progress, Typography, Tooltip, Button } from 'antd';
import { CloudUploadOutlined, CloseOutlined, CheckCircleOutlined, ExclamationCircleOutlined } from '@ant-design/icons';
import useUploadStore, { UploadTask } from '../../store/uploadSlice';

const { Text } = Typography;

// Helper function to format elapsed time
function formatElapsedTime(startTime: number): string {
  const seconds = Math.floor((Date.now() - startTime) / 1000);
  if (seconds < 60) return `${seconds}s`;
  const minutes = Math.floor(seconds / 60);
  return `${minutes}m ${seconds % 60}s`;
}

// TaskItem component defined outside main component to avoid recreation on each render
function TaskItem({ 
  task, 
  onRemove 
}: { 
  task: UploadTask; 
  onRemove: (id: string) => void;
}) {
  return (
    <div 
      style={{ 
        padding: '6px 8px', 
        background: task.status === 'uploading' ? '#2d2d2d' : 'transparent',
        borderRadius: 4,
        marginBottom: 4,
      }}
    >
      <div style={{ display: 'flex', alignItems: 'center', gap: 6, marginBottom: 4 }}>
        {task.status === 'uploading' && <CloudUploadOutlined style={{ color: '#52c41a', fontSize: 12 }} />}
        {task.status === 'completed' && <CheckCircleOutlined style={{ color: '#52c41a', fontSize: 12 }} />}
        {task.status === 'error' && <ExclamationCircleOutlined style={{ color: '#ff4d4f', fontSize: 12 }} />}
        <Tooltip title={task.name}>
          <Text 
            ellipsis 
            style={{ 
              fontSize: 11, 
              color: '#cccccc', 
              flex: 1, 
              maxWidth: 120,
            }}
          >
            {task.name}
          </Text>
        </Tooltip>
        {task.status !== 'uploading' && (
          <Button 
            type="text" 
            size="small" 
            icon={<CloseOutlined style={{ fontSize: 10 }} />} 
            onClick={() => onRemove(task.id)}
            style={{ padding: 0, color: '#666' }}
          />
        )}
      </div>
      {task.status === 'uploading' && (
        <Progress 
          percent={task.percent} 
          size="small" 
          strokeColor="#52c41a"
          trailColor="#3c3c3c"
          style={{ marginBottom: 0 }}
        />
      )}
      {task.status === 'uploading' && (
        <Text style={{ fontSize: 10, color: '#888', display: 'block' }}>
          {task.percent}% · {formatElapsedTime(task.startTime)}
        </Text>
      )}
    </div>
  );
}

export default function UploadProgressPanel() {
  const { tasks, removeTask, clearCompleted } = useUploadStore();

  // Filter active uploads (uploading status)
  const activeTasks = useMemo(() => 
    tasks.filter(t => t.status === 'uploading'), 
    [tasks]
  );

  // Get recent completed/error tasks (sorted by startTime, limit to 5)
  const recentTasks = useMemo(() => {
    const completed = tasks.filter(t => t.status !== 'uploading');
    // Sort by startTime descending (most recent first) and take last 5
    return completed.sort((a, b) => b.startTime - a.startTime).slice(0, 5);
  }, [tasks]);

  // Auto-clear old completed/error tasks after 60 seconds to prevent memory buildup
  useEffect(() => {
    const completedTasks = tasks.filter(t => t.status !== 'uploading');
    if (completedTasks.length > 5) {
      // Clear tasks older than 60 seconds
      const now = Date.now();
      completedTasks.forEach(t => {
        if (now - t.startTime > 60000) {
          removeTask(t.id);
        }
      });
    }
  }, [tasks, removeTask]);

  // If no tasks, don't render
  if (activeTasks.length === 0 && recentTasks.length === 0) {
    return null;
  }

  return (
    <div 
      style={{ 
        padding: '8px 12px',
        borderBottom: '1px solid #3c3c3c',
        maxHeight: 200,
        overflowY: 'auto',
      }}
    >
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 8 }}>
        <Text style={{ fontSize: 12, color: '#888' }}>
          {activeTasks.length > 0 ? `上传中 (${activeTasks.length})` : '上传记录'}
        </Text>
        {recentTasks.length > 0 && (
          <Button 
            type="text" 
            size="small" 
            onClick={clearCompleted}
            style={{ fontSize: 11, color: '#888', padding: 0 }}
          >
            清除
          </Button>
        )}
      </div>
      {activeTasks.map((task) => (
        <TaskItem key={task.id} task={task} onRemove={removeTask} />
      ))}
      {recentTasks.map((task) => (
        <TaskItem key={task.id} task={task} onRemove={removeTask} />
      ))}
    </div>
  );
}