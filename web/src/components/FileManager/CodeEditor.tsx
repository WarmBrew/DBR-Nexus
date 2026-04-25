import Editor from '@monaco-editor/react';

interface Props {
  value: string;
  language: string;
  onChange: (value: string) => void;
}

export default function CodeEditor({ value, language, onChange }: Props) {
  return (
    <Editor
      height="500px"
      language={language || 'plaintext'}
      value={value}
      onChange={(val) => onChange(val || '')}
      theme="vs-dark"
      options={{
        minimap: { enabled: false },
        fontSize: 14,
        wordWrap: 'on',
        scrollBeyondLastLine: false,
        automaticLayout: true,
      }}
    />
  );
}
